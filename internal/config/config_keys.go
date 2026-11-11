package config

import "fmt"

type namedKey struct{ name, value string }

// validateKeyRotation checks the optional keys of a rotation (see "Rotating
// keys" in doc/operations.md). A mistake here would not show at once: the
// server would start and sign-ins with a second factor would fail later.
func (c *Config) validateKeyRotation() error {
	session := []namedKey{{"JWT_SECRET", c.JWTSecret}, {"JWT_SECRET_PREVIOUS", c.JWTSecretPrevious}}
	data := []namedKey{{"DATA_KEY", c.DataKey}, {"DATA_KEY_PREVIOUS", c.DataKeyPrevious}}
	if c.DataKey == "" && c.DataKeyPrevious != "" {
		return fmt.Errorf("DATA_KEY_PREVIOUS is set without DATA_KEY: it takes the key you are replacing, and DATA_KEY the new one")
	}
	for _, pair := range [][]namedKey{session, data} {
		if pair[1].value != "" && pair[1].value == pair[0].value {
			return fmt.Errorf("%s is the same as %s: it takes the value you are replacing, and %s the new one", pair[1].name, pair[0].name, pair[0].name)
		}
	}
	if err := distinctFromSessionSecrets(data, session); err != nil {
		return err
	}
	if !c.IsProduction() {
		return nil
	}
	// The same strength as JWT_SECRET, which validateProductionSecrets checks.
	for _, k := range []namedKey{session[1], data[0], data[1]} {
		if k.value != "" && (len(k.value) < 32 || isPlaceholder(k.value)) {
			return fmt.Errorf("%s must be a random string of at least 32 characters in production when set, e.g. the output of `openssl rand -hex 32`", k.name)
		}
	}
	return nil
}

// distinctFromSessionSecrets refuses a data key that is a session secret:
// the two are separate so that either can be replaced on its own.
func distinctFromSessionSecrets(data, session []namedKey) error {
	for _, d := range data {
		for _, s := range session {
			if d.value != "" && d.value == s.value {
				return fmt.Errorf("%s must not be a session secret (it equals %s): a data key has its own value, e.g. the output of `openssl rand -hex 32`. What is sealed under the session secret stays readable without naming it", d.name, s.name)
			}
		}
	}
	return nil
}
