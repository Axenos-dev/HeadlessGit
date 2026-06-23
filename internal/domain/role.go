package domain

type Role string

const (
	RoleRead  Role = "read"
	RoleWrite Role = "write"
	RoleAdmin Role = "admin"
)

func (r Role) Level() int {
	switch r {
	case RoleRead:
		return 10
	case RoleWrite:
		return 20
	case RoleAdmin:
		return 30
	default:
		return 0 // unknown -> no access
	}
}

// whether r grants at least the access of other
func (r Role) AtLeast(other Role) bool {
	return r.Level() >= other.Level()
}
