package permissions

import (
	"errors"
	"time"

	"github.com/Axenos-dev/HeadlessGit/internal/domain"
)

type GrantPermissionRequest struct {
	UserID int64  `json:"userId"`
	Role   string `json:"role"`
}

func (r GrantPermissionRequest) Validate() error {
	if r.UserID == 0 {
		return errors.New("userId is required")
	}
	switch domain.Role(r.Role) {
	case domain.RoleRead, domain.RoleWrite, domain.RoleAdmin:
		return nil
	default:
		return errors.New("role must be 'read', 'write' or 'admin'")
	}
}

type PermissionResponse struct {
	UserID    int64      `json:"userId"`
	Role      string     `json:"role"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt *time.Time `json:"updatedAt,omitempty"`
}

func newPermission(p domain.Permission) PermissionResponse {
	return PermissionResponse{
		UserID:    p.UserID,
		Role:      string(p.Role),
		CreatedAt: p.CreatedAt,
		UpdatedAt: p.UpdatedAt,
	}
}

func newPermissions(perms []domain.Permission) []PermissionResponse {
	out := make([]PermissionResponse, len(perms))
	for i, p := range perms {
		out[i] = newPermission(p)
	}
	return out
}
