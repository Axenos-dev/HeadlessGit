package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/db/gen"
	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"go.uber.org/zap"
)

type Registry interface {
	CreateWebhook(ctx context.Context, repoID int64, secret, url string) (gen.Webhook, error)
	DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error
	ListWebhooksForRepository(ctx context.Context, repoID int64) ([]gen.Webhook, error)
}

type Service struct {
	registry Registry
	logger   *zap.Logger

	httpClient *http.Client
	eventsCh   chan domain.RepositoryEvent
}

func NewService(logger *zap.Logger, registry Registry) *Service {
	return &Service{
		logger:     logger,
		registry:   registry,
		eventsCh:   make(chan domain.RepositoryEvent, 1024),
		httpClient: http.DefaultClient,
	}
}

func (s *Service) RegisterWebhook(ctx context.Context, repoID int64, url string) (domain.Webhook, error) {
	secret, err := generateSecret()
	if err != nil {
		return domain.Webhook{}, err
	}

	webhook, err := s.registry.CreateWebhook(ctx, repoID, secret, url)
	if err != nil {
		return domain.Webhook{}, err
	}

	return toDomain(webhook), nil
}

func (s *Service) DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error {
	return s.registry.DeleteWebhook(ctx, webhookID, repositoryID)
}

func (s *Service) DispatchEvent(ctx context.Context, event domain.RepositoryEvent) error {
	select {
	case s.eventsCh <- event:
		return nil
	default:
		return ErrEventsChannelFull
	}
}

func (s *Service) Start(ctx context.Context, nWorkers int) {
	for range nWorkers {
		go s.handleEvents(ctx)
	}
}

func (s *Service) handleEvents(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-s.eventsCh:
			if err := s.handleEvent(ctx, event); err != nil {
				s.logger.Error("failed to handle event", zap.Any("event", event), zap.Error(err))
			}
		}
	}
}

func (s *Service) handleEvent(ctx context.Context, event domain.RepositoryEvent) error {
	webhooks, err := s.registry.ListWebhooksForRepository(ctx, event.RepositoryID)
	if err != nil {
		return err
	}

	for _, wh := range webhooks {
		func() {
			webhook := toDomain(wh)

			// send webhooks with retry (exponential backoff)
			err := withExponentialBackoff(ctx, 3, time.Second*2, func() error {
				// 15 second timeout on webhook request
				timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
				defer cancel()

				return s.sendWebhook(timeoutCtx, webhook, event)
			})
			if err != nil {
				s.logger.Warn(
					"failed to send webhook",
					zap.Any("webhookId", webhook.ID),
					zap.Any("webhookUrl", webhook.URL),
					zap.Any("event", event),
					zap.Error(err),
				)
			}
		}()
	}

	return nil
}

func (s *Service) sendWebhook(ctx context.Context, webhook domain.Webhook, event domain.RepositoryEvent) error {
	body, err := json.Marshal(WebhookPayload{
		Event:        event.Event,
		RepositoryID: event.RepositoryID,
		Ref:          event.Ref,
		OldSHA:       event.OldSHA,
		NewSHA:       event.NewSHA,
		PusherID:     event.PusherID,
	})
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "headlessgit")
	req.Header.Set("X-HeadlessGit-Delivery", deliveryID())
	// HMAC-SHA256 of the raw body, keyed by the webhook secret
	// receivers recompute it to verify the delivery is authentic
	req.Header.Set("X-HeadlessGit-Signature", "sha256="+sign(webhook.Secret, body))

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	if resp.StatusCode >= 400 {
		s.logger.Warn(
			"webhook endpoint returned error",
			zap.Int64("webhookId", webhook.ID),
			zap.Int("statusCode", resp.StatusCode),
			zap.String("respBody", string(respBody)),
		)
		return fmt.Errorf("webhook endpoint returned status %d", resp.StatusCode)
	}

	return nil
}

// sign returns the hex-encoded HMAC-SHA256 of body keyed by secret
func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

// deliveryID returns a random correlation id
func deliveryID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

func withExponentialBackoff(ctx context.Context, attempts int, base time.Duration, fn func() error) error {
	var err error
	for attempt := range attempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(base << (attempt - 1)):
			}
		}

		if err = fn(); err == nil {
			return nil
		}
	}
	return err
}

// random 32 bytes
func generateSecret() (raw string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	raw = base64.RawURLEncoding.EncodeToString(b)
	return raw, nil
}

func toDomain(webhook gen.Webhook) domain.Webhook {
	w := domain.Webhook{
		ID:           webhook.ID,
		RepositoryID: webhook.RepositoryID,
		URL:          webhook.Url,
		Secret:       webhook.Secret,
		CreatedAt:    time.UnixMilli(webhook.CreatedAtUnixMs).UTC(),
	}
	if webhook.UpdatedAtUnixMs.Valid {
		w.UpdatedAt = time.UnixMilli(webhook.UpdatedAtUnixMs.Int64).UTC()
	}

	return w
}
