package auth

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/msch128/mnema-talk/internal/httpx"
)

// This response is the sole explicit token wire conversion. Its nativeSecret
// wrapper allows JSON here while keeping all formatting and log values redacted.
type nativeWireSecret struct{ value nativeSecret }

func (nativeWireSecret) Format(state fmt.State, _ rune) {
	if _, err := io.WriteString(state, "[native wire secret redacted]"); err != nil {
		return
	}
}

func (nativeWireSecret) String() string { return "[native wire secret redacted]" }

func (nativeWireSecret) GoString() string { return "[native wire secret redacted]" }

func (nativeWireSecret) LogValue() slog.Value {
	return slog.StringValue("[native wire secret redacted]")
}

func (s nativeWireSecret) MarshalJSON() ([]byte, error) { return json.Marshal(s.value.wire()) }

type nativeGrantResponse struct {
	User             User             `json:"user"`
	FamilyID         uuid.UUID        `json:"family_id"`
	ClientInstanceID uuid.UUID        `json:"client_instance_id"`
	RefreshSequence  uint64           `json:"refresh_sequence"`
	AccessToken      nativeWireSecret `json:"access_token"`
	RefreshToken     nativeWireSecret `json:"refresh_token"`
	AccessExpiresAt  time.Time        `json:"access_expires_at"`
	FamilyExpiresAt  time.Time        `json:"family_expires_at"`
}

func (nativeGrantResponse) Format(state fmt.State, _ rune) {
	if _, err := io.WriteString(state, "[native wire grant redacted]"); err != nil {
		return
	}
}

func (nativeGrantResponse) String() string { return "[native wire grant redacted]" }

func (nativeGrantResponse) GoString() string { return "[native wire grant redacted]" }

func (nativeGrantResponse) LogValue() slog.Value {
	return slog.StringValue("[native wire grant redacted]")
}

func isValidNativeGrant(grant IssuedNative) bool {
	p := grant.Principal()
	if p.User().ID == uuid.Nil || p.FamilyID() == uuid.Nil || p.ClientInstanceID() == uuid.Nil {
		return false
	}
	if grant.RefreshSequence() > nativeMaxSequence {
		return false
	}
	return grant.access.wire() != "" && grant.refresh.wire() != ""
}

func writeNativeGrant(w http.ResponseWriter, grant IssuedNative) {
	if !isValidNativeGrant(grant) {
		httpx.WriteError(w, httpx.ErrInternal("invalid native grant"))
		return
	}

	p := grant.Principal()
	httpx.WriteJSON(w, http.StatusOK, nativeGrantResponse{User: grant.Principal().User(), FamilyID: p.FamilyID(), ClientInstanceID: p.ClientInstanceID(), RefreshSequence: grant.RefreshSequence(),
		AccessToken: nativeWireSecret{value: grant.access}, RefreshToken: nativeWireSecret{value: grant.refresh},
		AccessExpiresAt: grant.AccessExpiresAt(), FamilyExpiresAt: grant.FamilyExpiresAt()})
}
