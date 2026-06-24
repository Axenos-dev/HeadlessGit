package permissions

import (
	"errors"

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
