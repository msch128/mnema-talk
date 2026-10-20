package chat

import (
	"testing"

	"github.com/google/uuid"
)

func TestIsSameThreadContext(t *testing.T) {
	root1 := uuid.New()
	root2 := uuid.New()
	reply1 := uuid.New()

	// Both root messages
	if !isSameThreadContext(nil, nil, &reply1) {
		t.Errorf("expected both roots to be in same context")
	}

	// Both in same thread
	if !isSameThreadContext(&root1, &root1, &reply1) {
		t.Errorf("expected both in same thread root1 to match")
	}

	// Different threads
	if isSameThreadContext(&root1, &root2, &reply1) {
		t.Errorf("expected different thread roots to mismatch")
	}

	// Reply to thread root
	if !isSameThreadContext(nil, &root1, &root1) {
		t.Errorf("expected reply directly to thread root to match")
	}

	// Reply to a different root message from within thread
	if isSameThreadContext(nil, &root1, &root2) {
		t.Errorf("expected reply to unrelated root from within thread to mismatch")
	}

	// Root replying to a thread reply
	if isSameThreadContext(&root1, nil, &reply1) {
		t.Errorf("expected root replying to thread reply to mismatch")
	}
}

func TestDecodeEmbeds(t *testing.T) {
	// Empty or minimal
	if embeds, err := decodeEmbeds(nil); err != nil || embeds != nil {
		t.Errorf("expected nil embeds for nil raw, got %v, %v", embeds, err)
	}
	if embeds, err := decodeEmbeds([]byte("[]")); err != nil || embeds != nil {
		t.Errorf("expected nil embeds for empty array, got %v, %v", embeds, err)
	}

	// Valid embeds
	raw := []byte(`[{"title":"test","description":"hello"}]`)
	embeds, err := decodeEmbeds(raw)
	if err != nil {
		t.Fatalf("decode valid embeds failed: %v", err)
	}
	if len(embeds) != 1 || embeds[0].Title != "test" || embeds[0].Description != "hello" {
		t.Errorf("unexpected embeds decoded: %+v", embeds)
	}

	// Invalid JSON
	if _, err := decodeEmbeds([]byte(`[not json`)); err == nil {
		t.Errorf("expected error for malformed json")
	}
}
