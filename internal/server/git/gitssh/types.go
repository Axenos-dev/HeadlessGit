package gitssh

type lfsAuthResponse struct {
	Href   string            `json:"href"`
	Header map[string]string `json:"header"`
	// git-lfs uses this to know when to do re-auth
	ExpiresAt string `json:"expires_at,omitempty"` // RFC3339
}
