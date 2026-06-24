package domain

import "testing"

func TestRoleAtLeast(t *testing.T) {
	cases := []struct {
		role     Role
		required Role
		want     bool
	}{
		{RoleRead, RoleRead, true},
		{RoleWrite, RoleRead, true},
		{RoleAdmin, RoleRead, true},
		{RoleAdmin, RoleWrite, true},
		{RoleAdmin, RoleAdmin, true},
		{RoleRead, RoleWrite, false},
		{RoleWrite, RoleAdmin, false},
		{Role(""), RoleRead, false}, // no role grants nothing
	}

	for _, tc := range cases {
		if got := tc.role.AtLeast(tc.required); got != tc.want {
			t.Errorf("Role(%q).AtLeast(%q) = %v, want %v", tc.role, tc.required, got, tc.want)
		}
	}
}
