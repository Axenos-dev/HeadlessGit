package webhooks

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	webhookservice "github.com/Axenos-dev/HeadlessGit/internal/services/webhooks"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// fakeManager stubs WebhookManager for handler tests
type fakeManager struct {
	webhook domain.Webhook
	err     error
}

func (f fakeManager) RegisterWebhook(ctx context.Context, repoID int64, url string) (domain.Webhook, error) {
	return f.webhook, f.err
}

func (f fakeManager) DeleteWebhook(ctx context.Context, webhookID, repositoryID int64) error {
	return f.err
}

// newTestRouter mounts the handlers the same way the control server does
func newTestRouter(webhooks WebhookManager) http.Handler {
	r := chi.NewRouter()
	NewHandlers(zap.NewNop(), webhooks).RegisterRoutes(r)
	return r
}

func TestCreateWebhook(t *testing.T) {
	cases := []struct {
		name       string
		body       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"created", `{"url":"https://example.com/hook"}`, nil, http.StatusCreated, ""},
		{"duplicate", `{"url":"https://example.com/hook"}`, webhookservice.ErrWebhookExists, http.StatusConflict, "webhook_exists"},
		{"internal", `{"url":"https://example.com/hook"}`, io.ErrUnexpectedEOF, http.StatusInternalServerError, "internal_error"},
		{"invalid body", `not json`, nil, http.StatusBadRequest, "invalid_request"},
		{"missing url", `{}`, nil, http.StatusBadRequest, "invalid_request"},
		{"non-http url", `{"url":"ftp://example.com/hook"}`, nil, http.StatusBadRequest, "invalid_request"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := fakeManager{
				webhook: domain.Webhook{ID: 3, RepositoryID: 7, URL: "https://example.com/hook", Secret: "s"},
				err:     tc.svcErr,
			}
			rec := httptest.NewRecorder()
			newTestRouter(fake).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/repositories/7/webhooks", strings.NewReader(tc.body)))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode != "" {
				var body struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Error.Code != tc.wantCode {
					t.Errorf("code = %q, want %q", body.Error.Code, tc.wantCode)
				}
			}
		})
	}
}
