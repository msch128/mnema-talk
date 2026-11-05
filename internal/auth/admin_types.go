package auth

import (
	"time"

	"github.com/google/uuid"
)

// Request and response bodies of the member-admin routes (package memberadmin).
// They stay here, next to User, so the OpenAPI schema names and the generated
// web types do not change with the split.

// SetPasswordRequest is the body of POST /api/admin/users/{id}/password.
type SetPasswordRequest struct {
	Password string `json:"password" format:"password" minLength:"10"`
	OwnerProof
}

// TemporaryPassword is the response of POST /api/admin/users/{id}/password-reset.
type TemporaryPassword struct {
	// Password is shown once; it is never stored in plain text.
	Password string `json:"password"`
}

// TimeoutRequest is the body of POST /api/admin/users/{id}/timeout.
type TimeoutRequest struct {
	// DurationSeconds is how long the member is silenced, from now.
	DurationSeconds int `json:"duration_seconds" minimum:"5" maximum:"2419200"`
	// Reason is shown to the member. Optional.
	Reason string `json:"reason" maxLength:"200" binding:"optional"`
}

// TimeoutState is a member's timeout.
type TimeoutState struct {
	UserID uuid.UUID `json:"user_id" format:"uuid"`
	// TimeoutUntil is when it ends.
	TimeoutUntil time.Time `json:"timeout_until" format:"date-time"`
	Reason       string    `json:"reason"`
}

// Ban is an active ban as the admin list shows it.
type Ban struct {
	UserID      uuid.UUID `json:"user_id" format:"uuid"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name"`
	Reason      string    `json:"reason"`
	BannedAt    time.Time `json:"banned_at" format:"date-time"`
	// BannedUntil is null for a ban without an end.
	BannedUntil *time.Time `json:"banned_until" format:"date-time" extensions:"x-nullable"`
	// BannedBy names the moderator; empty when that account is gone.
	BannedBy string `json:"banned_by"`
}

// BanRequest is the body of POST /api/admin/users/{id}/ban.
type BanRequest struct {
	// Reason is shown to the member when they try to sign in; send an empty
	// string for none.
	Reason string `json:"reason" maxLength:"200"`
	// DurationSeconds ends the ban by itself after that long (60 seconds to
	// ten years); null or missing bans for good.
	DurationSeconds *int `json:"duration_seconds" extensions:"x-nullable" binding:"optional"`
	// RevokeInvite also revokes the invite the account registered with, so
	// that code cannot create the account anew (default true).
	RevokeInvite *bool `json:"revoke_invite" binding:"optional"`
}
