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
