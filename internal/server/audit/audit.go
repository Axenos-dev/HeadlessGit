package audit

import (
	"context"
	"time"

	"go.uber.org/zap"
)

// per-request audit record
type Event struct {
	RequestID  string
	Transport  string // "http" or "ssh"
	IdentityID int64  // resolved user/service account, 0 = anonymous
	RepoID     int64  // resolved repo, 0 = unresolved
	Command    string // git-upload-pack, git-receive-pack, lfs-batch, etc etc
	Result     string // ok, denied, error
}

type ctxKey struct{}

func NewContext(ctx context.Context, e *Event) context.Context {
	return context.WithValue(ctx, ctxKey{}, e)
}

func FromContext(ctx context.Context) *Event {
	e, _ := ctx.Value(ctxKey{}).(*Event)
	return e
}

func Log(logger *zap.Logger, e *Event, duration time.Duration) {
	logger.Info("request",
		zap.String("request_id", e.RequestID),
		zap.String("transport", e.Transport),
		zap.Int64("identity_id", e.IdentityID),
		zap.Int64("repo_id", e.RepoID),
		zap.String("git_command", e.Command),
		zap.String("result", e.Result),
		zap.Duration("duration", duration),
	)
}
