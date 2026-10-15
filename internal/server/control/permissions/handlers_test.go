package permissions

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	permsservice "github.com/Axenos-dev/HeadlessGit/internal/services/permissions"
	"go.uber.org/zap"
)

func TestPermissionsError(t *testing.T) {
	h := NewHandlers(zap.NewNop(), nil)

	invalid := h.permissionsError(fmt.Errorf("detail: %w", permsservice.ErrInvalidRole), "failed to grant permission")
	if invalid.Status != http.StatusBadRequest || invalid.Code != "invalid_request" {
		t.Errorf("invalid role = %d %s", invalid.Status, invalid.Code)
	}

	internal := h.permissionsError(errors.New("database is locked"), "failed to grant permission")
	if internal.Status != http.StatusInternalServerError || internal.Message != "failed to grant permission" {
		t.Errorf("internal = %d %q", internal.Status, internal.Message)
	}
}
