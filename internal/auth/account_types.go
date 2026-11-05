package auth

import "time"

// Request and response bodies of the account export and deletion routes
// (package account). They stay here, next to User, so the OpenAPI schema names
// and the generated web types do not change with the split.

// DeleteAccountRequest is the body of POST /api/users/me/delete.
type DeleteAccountRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// ExportStatus is where the caller's data export stands.
type ExportStatus struct {
	// State is none, pending, ready, failed, downloaded or expired.
	State       string     `json:"state" enums:"none,pending,ready,failed,downloaded,expired"`
	RequestedAt *time.Time `json:"requested_at,omitempty" binding:"optional" format:"date-time"`
	// ExpiresAt is when a ready export stops being downloadable.
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
	SizeBytes int64      `json:"size_bytes"`
	// NextRequestAt is when a new export may be requested; omitted if now.
	NextRequestAt *time.Time `json:"next_request_at,omitempty" binding:"optional" format:"date-time"`
}

// ExportRequest is the body of POST /api/users/me/export.
type ExportRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator or recovery code; needed only while
	// two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}
