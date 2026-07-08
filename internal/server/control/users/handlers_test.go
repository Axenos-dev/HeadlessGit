package users

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	usersservice "github.com/Axenos-dev/HeadlessGit/internal/services/users"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

// fakeUserManager stubs UserManager for handler tests: embed the interface
// and override only what the endpoint under test touches
type fakeUserManager struct {
	UserManager
	account domain.Account
	err     error
}

func (f fakeUserManager) Create(ctx context.Context, info domain.UserInfo) (domain.Account, error) {
	return f.account, f.err
}

func (f fakeUserManager) GetByUsername(ctx context.Context, username string) (domain.Account, error) {
	return f.account, f.err
}

// newTestRouter mounts the handlers the same way the control server does
func newTestRouter(users UserManager) http.Handler {
	r := chi.NewRouter()
	NewHandlers(zap.NewNop(), users, nil).RegisterRoutes(r)
	return r
}

func TestGetUserByUsername(t *testing.T) {
	cases := []struct {
		name       string
		target     string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"found", "/users/by-username/alice", nil, http.StatusOK, ""},
		{"numeric username", "/users/by-username/12345", nil, http.StatusOK, ""},
		{"not found", "/users/by-username/ghost", usersservice.ErrUserNotFound, http.StatusNotFound, "user_not_found"},
		{"internal", "/users/by-username/alice", context.DeadlineExceeded, http.StatusInternalServerError, "internal_error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			router := newTestRouter(fakeUserManager{
				account: domain.Account{UserID: 7, Username: "alice", Kind: domain.UserKindUser},
				err:     tc.svcErr,
			})

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tc.target, nil))

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tc.wantStatus, rec.Body.String())
			}
			if tc.wantCode == "" {
				var envelope struct {
					Data UserResponse `json:"data"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode data envelope: %v", err)
				}
				if envelope.Data.ID != 7 || envelope.Data.Username != "alice" {
					t.Errorf("body = %+v", envelope.Data)
				}
				return
			}
			var envelope struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode error envelope: %v", err)
			}
			if envelope.Error.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", envelope.Error.Code, tc.wantCode)
			}
		})
	}
}

func TestCreateUser(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		svcErr     error
		wantStatus int
		wantCode   string
	}{
		{"created", `{"username":"alice","kind":"user"}`, nil, http.StatusCreated, ""},
		{"duplicate", `{"username":"alice","kind":"user"}`, usersservice.ErrUserExists, http.StatusConflict, "user_exists"},
		{"service error", `{"username":"alice","kind":"user"}`, context.DeadlineExceeded, http.StatusInternalServerError, "internal_error"},
		{"invalid body", `not json`, nil, http.StatusBadRequest, "invalid_request"},
		{"missing username", `{"kind":"user"}`, nil, http.StatusBadRequest, "invalid_request"},
		{"bad kind", `{"username":"alice","kind":"robot"}`, nil, http.StatusBadRequest, "invalid_request"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router := newTestRouter(fakeUserManager{
				account: domain.Account{UserID: 7, Username: "alice", Kind: domain.UserKindUser},
				err:     tt.svcErr,
			})

			req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(tt.body))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d (body: %s)", rec.Code, tt.wantStatus, rec.Body.String())
			}
			if tt.wantCode != "" {
				var envelope struct {
					Error struct {
						Code string `json:"code"`
					} `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
					t.Fatalf("decode error envelope: %v", err)
				}
				if envelope.Error.Code != tt.wantCode {
					t.Errorf("code = %q, want %q", envelope.Error.Code, tt.wantCode)
				}
			}
		})
	}
}
