package gitssh

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

type Server struct {
	logger      *zap.Logger
	repoRoot    string
	hostKeyPath string
}

func NewServer(logger *zap.Logger, repoRoot, hostKeyPath string) *Server {
	return &Server{
		logger:      logger,
		repoRoot:    repoRoot,
		hostKeyPath: hostKeyPath,
	}
}

func (s *Server) Run(addr string) error {
	hostKey, err := s.loadOrCreateHostKey()
	if err != nil {
		return fmt.Errorf("host key: %w", err)
	}

	cfg := &ssh.ServerConfig{NoClientAuth: true} // no auth yet
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	s.logger.Info("git ssh listening", zap.String("addr", addr))

	for {
		conn, err := ln.Accept()
		if err != nil {
			return err
		}
		go s.handleConn(conn, cfg)
	}
}

func (s *Server) handleConn(nConn net.Conn, cfg *ssh.ServerConfig) {
	defer nConn.Close()

	sshConn, chans, reqs, err := ssh.NewServerConn(nConn, cfg)
	if err != nil {
		s.logger.Warn("ssh handshake failed", zap.Error(err))
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		// reject all of the incomming connections, if they are not sessions
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		go s.handleSession(newChan)
	}
}

func (s *Server) handleSession(newChan ssh.NewChannel) {
	ch, reqs, err := newChan.Accept()
	if err != nil {
		return
	}
	defer ch.Close()

	for req := range reqs {
		// git clients send a single "exec" request with the git command
		// so skip any non "exec"
		if req.Type != "exec" {
			req.Reply(false, nil)
			continue
		}

		var payload struct{ Command string }
		if err := ssh.Unmarshal(req.Payload, &payload); err != nil {
			req.Reply(false, nil)
			return
		}
		req.Reply(true, nil)

		s.runGit(ch, payload.Command)
		return
	}
}

func (s *Server) runGit(ch ssh.Channel, command string) {
	name, repo, err := parseGitCommand(command)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}

	repoPath, err := s.repoPath(repo)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}

	// execute command at repo path
	cmd := exec.Command(name, repoPath)
	// directly pass client's bytes to process stdin
	cmd.Stdin = ch
	cmd.Stdout = ch
	cmd.Stderr = ch.Stderr()

	code := 0
	if err := cmd.Run(); err != nil {
		s.logger.Warn("git command failed", zap.String("command", command), zap.Error(err))
		code = 1
	}
	sendExit(ch, code)
}

var allowedGitCommands = map[string]bool{
	"git-upload-pack":  true,
	"git-receive-pack": true,
}

// splits line to command name and repo name, and validates the input
func parseGitCommand(command string) (name, repo string, err error) {
	fields := strings.SplitN(strings.TrimSpace(command), " ", 2)
	if len(fields) != 2 {
		return "", "", errors.New("invalid git command")
	}

	// first argument is the command name
	name = fields[0]
	if !allowedGitCommands[name] {
		return "", "", fmt.Errorf("command not allowed: %s", name)
	}

	// second one is the repo name, wrapped in '
	repo = strings.Trim(fields[1], "'\"")
	return name, repo, nil
}

// repoPath resolves the requested repo under repoRoot and refuses to escape it.
func (s *Server) repoPath(repo string) (string, error) {
	root, err := filepath.Abs(s.repoRoot)
	if err != nil {
		return "", err
	}

	full := filepath.Join(root, filepath.Clean("/"+repo))
	if full != root && !strings.HasPrefix(full, root+string(filepath.Separator)) {
		return "", errors.New("invalid repo path")
	}
	return full, nil
}

func sendExit(ch ssh.Channel, code int) {
	status := make([]byte, 4)
	binary.BigEndian.PutUint32(status, uint32(code))
	ch.SendRequest("exit-status", false, status)
}

// reads the persisted host key, OR generating and saving a new one
func (s *Server) loadOrCreateHostKey() (ssh.Signer, error) {
	key, err := os.ReadFile(s.hostKeyPath)
	if err == nil {
		signer, err := ssh.ParsePrivateKey(key)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", s.hostKeyPath, err)
		}
		return signer, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	s.logger.Warn("ssh host key does not exist, generating new one", zap.String("path", s.hostKeyPath))
	return s.createHostKey()
}

func (s *Server) createHostKey() (ssh.Signer, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}

	block, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		return nil, err
	}

	if err := os.MkdirAll(filepath.Dir(s.hostKeyPath), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(s.hostKeyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		return nil, err
	}

	s.logger.Info("generated new ssh host key", zap.String("path", s.hostKeyPath))
	return ssh.NewSignerFromKey(priv)
}
