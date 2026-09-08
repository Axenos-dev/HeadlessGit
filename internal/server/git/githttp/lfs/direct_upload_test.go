package lfs

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
	"github.com/go-chi/chi/v5"
	"go.uber.org/zap"
)

type directUploadService struct {
	Service
	auth domain.LFSUploadAuthorization
	body []byte
}

func (s *directUploadService) Upload(_ context.Context, auth domain.LFSUploadAuthorization, body io.Reader) (domain.LFSUploadedObject, error) {
	s.auth = auth
	s.body, _ = io.ReadAll(body)
	return domain.LFSUploadedObject{OID: strings.Repeat("a", 64), Size: int64(len(s.body))}, nil
}

func TestDirectUploadHandler(t *testing.T) {
	service := &directUploadService{}
	router := chi.NewRouter()
	NewHandlers(zap.NewNop(), nil, nil, service).RegisterRoutes(router)
	expires := time.Now().Add(time.Minute).Unix()
	target := "/uploads/" + strings.Repeat("b", 64) + "?repositoryId=7&userId=42&size=4&expires=" + strconv.FormatInt(expires, 10) + "&signature=signed"

	req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader([]byte("data")))
	req.Header.Set("Content-Type", "application/octet-stream")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body.String())
	}
	if service.auth.RepositoryID != 7 || service.auth.UserID != 42 || service.auth.Size != 4 || service.auth.Signature != "signed" {
		t.Fatalf("authorization = %+v", service.auth)
	}
	if string(service.body) != "data" {
		t.Fatalf("body = %q", service.body)
	}
	var response struct {
		Data uploadedObject `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.Size != 4 || len(response.Data.OID) != 64 {
		t.Fatalf("response = %+v", response.Data)
	}
}

func TestDirectUploadHandlerRejectsInvalidBody(t *testing.T) {
	router := chi.NewRouter()
	NewHandlers(zap.NewNop(), nil, nil, &directUploadService{}).RegisterRoutes(router)
	target := "/uploads/" + strings.Repeat("b", 64) + "?repositoryId=7&userId=42&size=4&expires=1&signature=signed"

	for _, contentType := range []string{"", "multipart/form-data"} {
		req := httptest.NewRequest(http.MethodPut, target, bytes.NewReader([]byte("data")))
		req.Header.Set("Content-Type", contentType)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("content type %q: status = %d", contentType, rec.Code)
		}
	}
}
