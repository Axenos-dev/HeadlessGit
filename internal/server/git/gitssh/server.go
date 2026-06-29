package gitssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/Axenos-dev/HeadlessGit/internal/gitbackend"
	"github.com/Axenos-dev/HeadlessGit/internal/server/audit"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
)

// how long an LFS bearer token handed back over SSH stays valid
const lfsTokenTTL = 15 * time.Minute

type GitBackend interface {
	UploadPack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) error
	ReceivePack(ctx context.Context, storagePath string, stateless bool, stdin io.Reader, stdout, stderr io.Writer) ([]gitbackend.RefChange, error)
}

type Dispatcher interface {
	DispatchEvent(ctx context.Context, event domain.RepositoryEvent) error
}

type RepositoryResolver interface {
	GetRepositoryByPath(ctx context.Context, namespace, name string) (domain.Repository, error)
}

type Authenticator interface {
	AuthenticateSSHKey(ctx context.Context, fingerprint string) (domain.Account, error)
}

type Authorizer interface {
	Authorize(ctx context.Context, account *domain.Account, repo domain.Repository, required domain.Role) error
}

type TokenMinter interface {
	MintToken(ctx context.Context, userID int64, title string, expiresAt *time.Time) (string, domain.Token, error)
}

type LFSEndpoints interface {
	LFSEndpoint(namespace, name string) string
}

type Services struct {
	Backend        GitBackend
	Resolver       RepositoryResolver
	Authentication Authenticator
	Authorization  Authorizer
	Minter         TokenMinter
	LFS            LFSEndpoints
	Dispatcher     Dispatcher
}

type Server struct {
	logger      *zap.Logger
	hostKeyPath string

	backend    GitBackend
	resolver   RepositoryResolver
	auth       Authenticator
	authz      Authorizer
	minter     TokenMinter
	lfs        LFSEndpoints // nil if LFS is disabled
	dispatcher Dispatcher
}

func NewServer(logger *zap.Logger, hostKeyPath string, svc Services) *Server {
	return &Server{
		logger:      logger,
		hostKeyPath: hostKeyPath,
		backend:     svc.Backend,
		resolver:    svc.Resolver,
		auth:        svc.Authentication,
		authz:       svc.Authorization,
		minter:      svc.Minter,
		lfs:         svc.LFS,
		dispatcher:  svc.Dispatcher,
	}
}

func (s *Server) Run(ctx context.Context, addr string) error {
	hostKey, err := s.loadOrCreateHostKey()
	if err != nil {
		return fmt.Errorf("host key: %w", err)
	}

	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// just resolve the key to a user
			account, err := s.auth.AuthenticateSSHKey(context.Background(), ssh.FingerprintSHA256(key))
			if err != nil {
				return nil, errors.New("unauthorized")
			}
			// carry the resolved identity to the session via Permissions.Extensions
			return &ssh.Permissions{Extensions: map[string]string{
				"account_id": strconv.FormatInt(account.UserID, 10),
				"username":   account.Username,
				"kind":       string(account.Kind),
			}}, nil
		},
	}
	cfg.AddHostKey(hostKey)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}

	// stop accepting new connections on context done
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	s.logger.Info("git ssh listening", zap.String("addr", addr))

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil // listener closed
			}

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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		// cancel on connection shutdown
		sshConn.Wait()
		cancel()
	}()

	// resolved during the handshake by PublicKeyCallback
	account := accountFromPermissions(sshConn.Permissions)

	go ssh.DiscardRequests(reqs)

	for newChan := range chans {
		// reject all of the incomming connections, if they are not sessions
		if newChan.ChannelType() != "session" {
			newChan.Reject(ssh.UnknownChannelType, "only session channels are supported")
			continue
		}
		go s.handleSession(ctx, account, newChan)
	}
}

func (s *Server) handleSession(ctx context.Context, account domain.Account, newChan ssh.NewChannel) {
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

		s.handleExec(ctx, account, ch, payload.Command)
		return
	}
}

// dispatches an exec request to the matching handler,
// or git-upload/receive-pack or git-lfs-authenticate
func (s *Server) handleExec(ctx context.Context, account domain.Account, ch ssh.Channel, command string) {
	subcommand, _, _ := strings.Cut(strings.TrimSpace(command), " ")
	if subcommand == "git-lfs-authenticate" {
		s.runLFSAuthenticate(ctx, account, ch, command)
		return
	}

	s.runGit(ctx, account, ch, command)
}

func (s *Server) runGit(ctx context.Context, account domain.Account, ch ssh.Channel, command string) {
	// initiate empty audit event
	start := time.Now()
	e := &audit.Event{
		RequestID:  newRequestID(),
		Transport:  "ssh",
		IdentityID: account.UserID,
		Result:     "error", // overwritten later
	}
	// on defer we send the log
	defer func() {
		// it HAS to be wrapped in func() {}, so .Since would work properly
		audit.Log(s.logger, e, time.Since(start))
	}()

	subcommand, repo, err := parseGitCommand(command)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}
	e.Command = subcommand

	namespace, name, err := splitRepoPath(repo)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}

	// resolve repository by the requested path
	resolved, err := s.resolver.GetRepositoryByPath(ctx, namespace, name)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "repository not found")
		sendExit(ch, 1)
		return
	}
	e.RepoID = resolved.ID

	// upload-pack reads, receive-pack writes
	var required domain.Role
	switch subcommand {
	case "git-upload-pack":
		required = domain.RoleRead
	case "git-receive-pack":
		required = domain.RoleWrite
	default:
		fmt.Fprintf(ch.Stderr(), "unhandled git subcommand: %s\n", subcommand)
		sendExit(ch, 1)
		return
	}

	if err := s.authz.Authorize(ctx, &account, resolved, required); err != nil {
		e.Result = "denied"
		fmt.Fprintln(ch.Stderr(), "access denied")
		sendExit(ch, 1)
		return
	}

	switch subcommand {
	case "git-upload-pack":
		err = s.backend.UploadPack(ctx, resolved.StoragePath, false, ch, ch, ch.Stderr())
	case "git-receive-pack":
		var changes []gitbackend.RefChange
		changes, err = s.backend.ReceivePack(ctx, resolved.StoragePath, false, ch, ch, ch.Stderr())
		if err == nil {
			s.dispatchPush(ctx, resolved, namespace, account, changes)
		}
	}
	if err != nil {
		s.logger.Warn("git command failed", zap.String("command", command), zap.Error(err))
		sendExit(ch, 1)
		return
	}

	e.Result = "ok"
	sendExit(ch, 0)
}

func (s *Server) dispatchPush(ctx context.Context, repo domain.Repository, namespace string, account domain.Account, changes []gitbackend.RefChange) {
	if s.dispatcher == nil {
		return
	}

	fullName := namespace + "/" + repo.RepositoryName

	for _, c := range changes {
		err := s.dispatcher.DispatchEvent(ctx, domain.RepositoryEvent{
			Event:              "push",
			RepositoryID:       repo.ID,
			RepositoryName:     repo.RepositoryName,
			RepositoryFullName: fullName,
			PusherID:           account.UserID,
			PusherUsername:     account.Username,
			Ref:                c.Ref,
			OldSHA:             c.OldSHA,
			NewSHA:             c.NewSHA,
			Timestamp:          time.Now().UTC(),
		})
		if err != nil {
			s.logger.Warn("failed to enqueue webhook event", zap.String("ref", c.Ref), zap.Error(err))
		}
	}
}

func (s *Server) runLFSAuthenticate(ctx context.Context, account domain.Account, ch ssh.Channel, command string) {
	start := time.Now()
	e := &audit.Event{
		RequestID:  newRequestID(),
		Transport:  "ssh",
		IdentityID: account.UserID,
		Command:    "git-lfs-authenticate",
		Result:     "error", // overwritten later
	}
	defer func() {
		audit.Log(s.logger, e, time.Since(start))
	}()

	if s.lfs == nil {
		fmt.Fprintln(ch.Stderr(), "git-lfs is not enabled")
		sendExit(ch, 1)
		return
	}

	repoPath, operation, err := parseLFSAuthCommand(command)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}

	namespace, name, err := splitRepoPath(repoPath)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), err.Error())
		sendExit(ch, 1)
		return
	}

	resolved, err := s.resolver.GetRepositoryByPath(ctx, namespace, name)
	if err != nil {
		fmt.Fprintln(ch.Stderr(), "repository not found")
		sendExit(ch, 1)
		return
	}
	e.RepoID = resolved.ID

	var required domain.Role
	switch operation {
	// for download we need at least read role
	case "download":
		required = domain.RoleRead
	// for upload we need at least write role
	case "upload":
		required = domain.RoleWrite
	default:
		fmt.Fprintf(ch.Stderr(), "unsupported lfs operation: %s\n", operation)
		sendExit(ch, 1)
		return
	}

	if err := s.authz.Authorize(ctx, &account, resolved, required); err != nil {
		e.Result = "denied"
		fmt.Fprintln(ch.Stderr(), "access denied")
		sendExit(ch, 1)
		return
	}

	expiresAt := time.Now().Add(lfsTokenTTL)
	rawToken, _, err := s.minter.MintToken(ctx, account.UserID, "lfs-ssh", &expiresAt)
	if err != nil {
		s.logger.Warn("failed to mint lfs token", zap.Error(err))
		fmt.Fprintln(ch.Stderr(), "failed to issue lfs token")
		sendExit(ch, 1)
		return
	}

	resp := lfsAuthResponse{
		Href:      s.lfs.LFSEndpoint(namespace, name),
		Header:    map[string]string{"Authorization": "Bearer " + rawToken},
		ExpiresAt: expiresAt.UTC().Format(time.RFC3339),
	}
	if err := json.NewEncoder(ch).Encode(resp); err != nil {
		s.logger.Warn("failed to write lfs auth response", zap.Error(err))
		sendExit(ch, 1)
		return
	}

	e.Result = "ok"
	sendExit(ch, 0)
}

// splits line to command name and repo name, and validates the input
func parseGitCommand(command string) (subcommand, repo string, err error) {
	fields := strings.SplitN(strings.TrimSpace(command), " ", 2)
	if len(fields) != 2 {
		return "", "", errors.New("invalid git command")
	}

	// first argument is the command name
	subcommand = fields[0]

	// second one is the repo name, wrapped in '
	repo = strings.Trim(fields[1], "'\"")
	return subcommand, repo, nil
}

// splits git-lfs-authenticate <path> <operation>
func parseLFSAuthCommand(command string) (repoPath, operation string, err error) {
	fields := strings.Fields(strings.TrimSpace(command))
	if len(fields) != 3 || fields[0] != "git-lfs-authenticate" {
		return "", "", errors.New("invalid git-lfs-authenticate command")
	}

	repoPath = strings.Trim(fields[1], "'\"")
	operation = fields[2]
	return repoPath, operation, nil
}

// accountFromPermissions reconstructs the identity stashed by PublicKeyCallback.
func accountFromPermissions(p *ssh.Permissions) domain.Account {
	if p == nil {
		return domain.Account{}
	}
	id, _ := strconv.ParseInt(p.Extensions["account_id"], 10, 64)
	return domain.Account{
		UserID:   id,
		Username: p.Extensions["username"],
		Kind:     domain.UserKind(p.Extensions["kind"]),
	}
}

// splits "namespace/name(.git)" into its parts
func splitRepoPath(repo string) (namespace, name string, err error) {
	repo = strings.Trim(repo, "/")
	repo = strings.TrimSuffix(repo, ".git")

	parts := strings.Split(repo, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", errors.New("invalid repository path")
	}
	return parts[0], parts[1], nil
}

func sendExit(ch ssh.Channel, code int) {
	status := make([]byte, 4)
	binary.BigEndian.PutUint32(status, uint32(code))
	ch.SendRequest("exit-status", false, status)
}

func newRequestID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
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
