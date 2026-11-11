package main

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/msch128/mnema-talk/internal/auth"
	"github.com/msch128/mnema-talk/internal/db"
)

// unreadableSecretsFix is what an operator does about authenticator secrets
// no configured key opens.
const unreadableSecretsFix = "put the key they were sealed with back (DATA_KEY, or the old value in DATA_KEY_PREVIOUS or JWT_SECRET_PREVIOUS), run `docker compose up -d` and then `mnema-talk rotate-keys`, which names the accounts; or reset them with `mnema-talk reset-2fa <username>`. See \"Rotating keys\" in doc/operations.md"

// reportStoredSecrets logs which key seals the second factor's data, by a
// fingerprint that `mnema-talk rotate-keys` prints too, and says loudly when
// stored authenticator secrets cannot be opened with the configured keys:
// those members cannot pass the second step of sign-in. The start goes on,
// everyone else is unaffected. Nothing secret and no account name is logged.
func reportStoredSecrets(ctx context.Context, s *auth.Sessions, q db.Querier, log *slog.Logger) {
	keys := s.KeySetup()
	log.Info("authenticator secrets are sealed under "+keys.Sealing, "fingerprint", keys.Fingerprint, "previous_keys", keys.Previous)
	found, err := s.CheckStoredSecrets(ctx, q)
	if err != nil {
		log.Warn("could not check the stored authenticator secrets", "err", err)
		return
	}
	if found.Unreadable > 0 {
		log.Error("AUTHENTICATOR SECRETS CANNOT BE READ with any configured key: these accounts cannot sign in with their authenticator code",
			"accounts", found.Unreadable, "name_a_data_key_that_is_not_configured", found.UnknownDataKey, "fix", unreadableSecretsFix)
	}
}

// storedSecretsNote is the doctor's line about the same check; empty when
// every secret opens.
func storedSecretsNote(found auth.StoredSecrets) string {
	if found.Unreadable == 0 {
		return ""
	}
	note := fmt.Sprintf("[FAIL] %d account(s) with two-factor sign-in have an authenticator secret that no configured key can read; they cannot sign in with their code.", found.Unreadable)
	if found.UnknownDataKey > 0 {
		note += fmt.Sprintf(" %d of them name a data key that is not configured: DATA_KEY was removed or replaced without DATA_KEY_PREVIOUS.", found.UnknownDataKey)
	}
	return note + "\n       fix: " + unreadableSecretsFix + "."
}
