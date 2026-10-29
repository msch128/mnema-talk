package auth

import (
	"encoding/json"
	"testing"
	"time"
)

func TestUpdateInviteRequestTellsNullFromAbsent(t *testing.T) {
	cases := []struct {
		body             string
		setExp, setUses  bool
		wantUses         *int
		wantExpiresIsNil bool
	}{
		{`{}`, false, false, nil, true},
		{`{"max_uses":null}`, false, true, nil, true},
		{`{"expires_at":null}`, true, false, nil, true},
		{`{"max_uses":7,"expires_at":"2030-01-02T03:04:05Z"}`, true, true, new(int), false},
	}
	for _, c := range cases {
		var req UpdateInviteRequest
		if err := json.Unmarshal([]byte(c.body), &req); err != nil {
			t.Fatalf("%s: %v", c.body, err)
		}
		if req.setExpiresAt != c.setExp || req.setMaxUses != c.setUses || (req.ExpiresAt == nil) != c.wantExpiresIsNil {
			t.Errorf("%s: %+v", c.body, req)
		}
		if c.wantUses != nil && (req.MaxUses == nil || *req.MaxUses != 7) {
			t.Errorf("%s: max_uses %v", c.body, req.MaxUses)
		}
	}
	for _, bad := range []string{`{"code":"x"}`, `{"max_uses":"7"}`, `[]`, `{"expires_at":"soon"}`} {
		var req UpdateInviteRequest
		if err := json.Unmarshal([]byte(bad), &req); err == nil {
			t.Errorf("%s was accepted", bad)
		}
	}
}

func TestValidateNewExpiry(t *testing.T) {
	now := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	for name, tc := range map[string]struct {
		in *time.Time
		ok bool
	}{
		"none removes it":  {nil, true},
		"a minute ahead":   {at(time.Minute), true},
		"exactly now":      {at(0), false},
		"in the past":      {at(-time.Hour), false},
		"at the bound":     {at(maxInviteExpiry), true},
		"beyond the bound": {at(maxInviteExpiry + time.Second), false},
	} {
		if err := validateNewExpiry(tc.in, now); (err == nil) != tc.ok {
			t.Errorf("%s: %v", name, err)
		}
	}
}
