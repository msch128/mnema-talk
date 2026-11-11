package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type inviteEntropyFailure struct{}

func (inviteEntropyFailure) Read([]byte) (int, error) { return 0, errors.New("test entropy failure") }

func TestInviteEntropyFailureIsPropagated(t *testing.T) {
	// GenerateInviteCode uses rand.Int's injectable Reader, unlike rand.Read
	// (which terminates the process on entropy failure in modern Go).
	original := rand.Reader
	rand.Reader = inviteEntropyFailure{}
	t.Cleanup(func() { rand.Reader = original })
	if code, err := GenerateInviteCode(); code != "" || err == nil || err.Error() != "test entropy failure" {
		t.Fatalf("generate = %q, %v", code, err)
	}
	if inv, err := CreateInvite(context.Background(), nil, uuid.Nil, "", nil, nil); inv != nil || err == nil || err.Error() != "test entropy failure" {
		t.Fatalf("create = %+v, %v", inv, err)
	}
}
