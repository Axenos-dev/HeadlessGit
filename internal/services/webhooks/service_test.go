package webhooks

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"go.uber.org/zap"
)

type fakeRegistry struct {
	Registry
	webhook gen.Webhook
	err     error
}

func (f fakeRegistry) CreateWebhook(ctx context.Context, repoID int64, secret, url string) (gen.Webhook, error) {
	return f.webhook, f.err
}

func (f fakeRegistry) ListWebhooksForRepository(ctx context.Context, repoID int64) ([]gen.Webhook, error) {
	if f.err != nil {
		return nil, f.err
	}
	return []gen.Webhook{f.webhook}, nil
}

func TestRegisterWebhook(t *testing.T) {
	t.Run("ok", func(t *testing.T) {
		svc := NewService(zap.NewNop(), fakeRegistry{webhook: gen.Webhook{ID: 3, RepositoryID: 7, Url: "https://example.com/hook", Secret: "s"}})
		webhook, err := svc.RegisterWebhook(context.Background(), 7, "https://example.com/hook")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if webhook.ID != 3 || webhook.URL != "https://example.com/hook" {
			t.Errorf("unexpected webhook: %+v", webhook)
		}
	})

	t.Run("duplicate", func(t *testing.T) {
		// the insert is "on conflict do nothing returning *" -> on duplicate "no rows"
		svc := NewService(zap.NewNop(), fakeRegistry{err: sql.ErrNoRows})
		if _, err := svc.RegisterWebhook(context.Background(), 7, "https://example.com/hook"); !errors.Is(err, ErrWebhookExists) {
			t.Errorf("want ErrWebhookExists, got %v", err)
		}
	})

	t.Run("registry error", func(t *testing.T) {
		boom := errors.New("boom")
		svc := NewService(zap.NewNop(), fakeRegistry{err: boom})
		if _, err := svc.RegisterWebhook(context.Background(), 7, "https://example.com/hook"); !errors.Is(err, boom) {
			t.Errorf("want boom, got %v", err)
		}
	})
}

func TestListWebhooks(t *testing.T) {
	svc := NewService(zap.NewNop(), fakeRegistry{webhook: gen.Webhook{ID: 3, RepositoryID: 7, Url: "https://example.com/hook", Secret: "s"}})
	webhooks, err := svc.ListWebhooks(context.Background(), 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(webhooks) != 1 || webhooks[0].ID != 3 || webhooks[0].URL != "https://example.com/hook" {
		t.Errorf("unexpected webhooks: %+v", webhooks)
	}
}
