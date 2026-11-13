package auth

import (
	"time"

	"github.com/google/uuid"
)

// Request and response bodies of the sign-in, profile, session, e-mail,
// two-factor, account export/deletion and member-admin routes (packages authn,
// account, memberadmin). They stay here, next to User, so the OpenAPI schema
// names and the generated web types do not change with the split.

// AccountInfo is what an account knows about its own sign-in security. It is
// only ever returned to the account itself.
type AccountInfo struct {
	// Email is the caller's own address; omitted when none is set. It is never
	// shown to other members or administrators.
	Email string `json:"email,omitempty" binding:"optional"`
	// EmailVerified is true once the address was confirmed by its link.
	EmailVerified bool `json:"email_verified"`
	// MailAvailable is true when the server can send e-mail (SMTP is
	// configured): addresses can be confirmed and passwords reset by mail.
	MailAvailable bool `json:"mail_available"`
	// TwoFactorEnabled is true while sign-in asks for an authenticator code.
	TwoFactorEnabled bool `json:"two_factor_enabled"`
	// RecoveryCodesRemaining is how many single-use recovery codes are unused
	// (0 when two-factor sign-in is off).
	RecoveryCodesRemaining int `json:"recovery_codes_remaining"`
	// ConfirmationSent is only present in the answer to PUT /api/users/me/email
	// for a new address: false when the address was saved but the
	// confirmation mail was refused (too many mails or SMTP failure), so the
	// client can offer to send it again. Omitted when no mail was due.
	ConfirmationSent *bool `json:"confirmation_sent,omitempty" binding:"optional"`
}

// SetEmailRequest is the body of PUT /api/users/me/email.
type SetEmailRequest struct {
	Email           string `json:"email" format:"email" maxLength:"254"`
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// PasswordConfirmRequest carries the caller's current password and, while
// two-factor sign-in is on, a current code.
type PasswordConfirmRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// VerifyEmailRequest is the body of POST /api/auth/email/verify.
type VerifyEmailRequest struct {
	// Token is the one from the link in the confirmation mail.
	Token string `json:"token" minLength:"43" maxLength:"43"`
}

// LoginRequest is the body of POST /api/auth/login.
type LoginRequest struct {
	// Username is case-insensitive.
	Username string `json:"username"`
	Password string `json:"password" format:"password"`
}

// RegisterRequest is the body of POST /api/auth/register.
type RegisterRequest struct {
	// Username: 2-32 characters, lowercase letters, digits, '_' and '.', no leading, trailing or repeated '.'. Spaces become '_' and capitals lower-case before checking. Unique regardless of case; "all" and "here" are reserved. Accounts created before this rule keep their names.
	Username string `json:"username"`
	// DisplayName defaults to the username.
	DisplayName string `json:"display_name" maxLength:"24" binding:"optional"`
	// Password needs 10 characters at least and 72 bytes at most.
	Password   string `json:"password" format:"password" minLength:"10"`
	InviteCode string `json:"invite_code" maxLength:"64"`
}

// UserEnvelope wraps the signed-in user in login and register responses.
type UserEnvelope struct {
	User User `json:"user"`
}

// ChangePasswordRequest is the body of PUT /api/auth/password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	NewPassword     string `json:"new_password" format:"password" minLength:"10"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while two-factor sign-in is on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// UpdateProfileRequest is the body of PUT /api/users/me/profile.
type UpdateProfileRequest struct {
	// DisplayName: empty resets it to the username.
	DisplayName string `json:"display_name" maxLength:"24" binding:"optional"`
	Bio         string `json:"bio" maxLength:"250" binding:"optional"`
}

// SetLocaleRequest is the body of PUT /api/users/me/locale.
type SetLocaleRequest struct {
	Locale string `json:"locale" enums:"de,en"`
}

// SetPresenceRequest is the body of PUT /api/users/me/presence.
type SetPresenceRequest struct {
	// Presence is the chosen presence; "offline" is never chosen, it follows
	// from having no open WebSocket.
	Presence string `json:"presence" enums:"online,away,dnd,focus"`
}

// SetStatusRequest is the body of the status-line endpoints.
type SetStatusRequest struct {
	// StatusText: empty clears the status.
	StatusText string `json:"status_text" maxLength:"32"`
	// ExpiresAt makes the status clear itself at this absolute time (the client
	// computes it from the member's time zone). Omitted or null: it stays until
	// changed. Must lie in the future, at most a year ahead; ignored when the
	// status is cleared and when an admin sets another member's status.
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
}

// OwnerProof is the Owner's confirmation of their own identity for taking over
// another account. Both fields are optional in the schema so existing clients
// keep validating, but the server refuses the request without them.
type OwnerProof struct {
	// CurrentPassword is the Owner's own password.
	CurrentPassword string `json:"current_password,omitempty" format:"password" binding:"optional"`
	// Code is a current authenticator code or an unused recovery code; needed
	// only while the Owner has two-factor sign-in on.
	Code string `json:"code,omitempty" maxLength:"32" binding:"optional"`
}

// AuthOptions tells the sign-in screen which recovery paths exist.
type AuthOptions struct {
	// PasswordReset is true when members can reset a forgotten password by
	// e-mail (the server can send mail). It does not say who has an address.
	PasswordReset bool `json:"password_reset"`
	// CommunityName is what the sign-in screen calls the community: the Owner's
	// name for it, else the host. Public, like the rest of this answer.
	CommunityName string `json:"community_name" binding:"optional"`
	// CommunityIcon is the version of the community icon (GET
	// /api/community/icon), left out while the Owner set none. Public as well.
	CommunityIcon string `json:"community_icon,omitempty" binding:"optional"`
}

// PasswordResetRequest is the body of POST /api/auth/password-reset/request.
type PasswordResetRequest struct {
	Email string `json:"email" format:"email" maxLength:"254"`
}

// PasswordResetConfirmation is the body of POST /api/auth/password-reset/confirm.
type PasswordResetConfirmation struct {
	// Token is the one from the link in the reset mail.
	Token       string `json:"token" minLength:"43" maxLength:"43"`
	NewPassword string `json:"new_password" format:"password" minLength:"10"`
}

// TwoFactorSetupRequest is the body of POST /api/users/me/2fa/setup.
type TwoFactorSetupRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
}

// TwoFactorSetup carries the new authenticator secret.
type TwoFactorSetup struct {
	// Secret is the shared secret in base32, for typing into an app by hand.
	Secret string `json:"secret"`
	// OtpauthURI is the same secret as an otpauth:// URI, for the QR code.
	OtpauthURI string `json:"otpauth_uri"`
}

// TwoFactorEnableRequest is the body of POST /api/users/me/2fa/enable.
type TwoFactorEnableRequest struct {
	// Code is the six digits the app shows for the new secret.
	Code string `json:"code" maxLength:"32"`
}

// RecoveryCodes are shown once.
type RecoveryCodes struct {
	RecoveryCodes []string `json:"recovery_codes"`
}

// TwoFactorProofRequest asks for the password and a current code (or an
// unused recovery code).
type TwoFactorProofRequest struct {
	CurrentPassword string `json:"current_password" format:"password"`
	Code            string `json:"code" maxLength:"32"`
}

// TwoFactorChallenge is the answer to a correct password for an account that
// has two-factor sign-in on. It is not a session: the sign-in is finished by
// POST /api/auth/login/2fa.
type TwoFactorChallenge struct {
	TwoFactorRequired bool `json:"two_factor_required"`
	// Challenge is a single-use token bound to the account and to this
	// sign-in; it is worth nothing without a valid code.
	Challenge string `json:"challenge" minLength:"43" maxLength:"43"`
	// ExpiresIn is the number of seconds the challenge stays open.
	ExpiresIn int `json:"expires_in"`
}

// TwoFactorLoginRequest is the body of POST /api/auth/login/2fa.
type TwoFactorLoginRequest struct {
	Challenge string `json:"challenge" minLength:"43" maxLength:"43"`
	// Code is the six digits of the authenticator app, or an unused recovery
	// code (spaces and dashes are ignored).
	Code string `json:"code" maxLength:"32"`
}

// MemberSession is one of the caller's own sessions on this server.
type MemberSession struct {
	ID uuid.UUID `json:"id" format:"uuid"`
	// Device is a coarse label ("Firefox · Linux") parsed from the user
	// agent, empty when it is not recognised. No address and no raw user
	// agent are stored.
	Device    string    `json:"device" maxLength:"64"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
	// LastActiveAt is when the session last made an authenticated request,
	// accurate to a few minutes.
	LastActiveAt time.Time `json:"last_active_at" format:"date-time"`
	// Current marks the session this request was made with.
	Current bool `json:"current"`
	// Push tells whether the session's device receives Web Push notifications.
	Push bool `json:"push"`
}

// MemberSessionList is the response of GET /api/auth/sessions.
type MemberSessionList struct {
	Sessions []MemberSession `json:"sessions"`
}

// RevokedSessions is the response of POST /api/auth/sessions/revoke-others.
type RevokedSessions struct {
	Revoked int `json:"revoked" minimum:"0"`
}

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
	// ExpiresAt is when a ready export stops being downloadable (after a download, the end of the hour in which it can be downloaded again).
	ExpiresAt *time.Time `json:"expires_at,omitempty" binding:"optional" format:"date-time"`
	// DownloadedAt is when the export was first downloaded completely.
	DownloadedAt *time.Time `json:"downloaded_at,omitempty" binding:"optional" format:"date-time"`
	SizeBytes    int64      `json:"size_bytes"`
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
