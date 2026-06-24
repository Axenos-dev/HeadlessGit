package domain

type UserKind string

const (
	UserKindUser    UserKind = "user"
	UserKindService UserKind = "service"
)

type UserInfo struct {
	Username string
	Kind     UserKind
}

// full user information(including ID)
type Account struct {
	UserID   int64
	Username string
	Kind     UserKind
	IsAdmin  bool
}
