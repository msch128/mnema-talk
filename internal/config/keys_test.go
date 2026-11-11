package config

import (
	"strings"
	"testing"
)

func keyEnv(extra map[string]string) func(string) (string, bool) {
	env := map[string]string{"DATABASE_URL": "postgres://u:p@localhost/db", "JWT_SECRET": strings.Repeat("j", 40)}
	for k, v := range extra {
		env[k] = v
	}
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}

func productionKeyEnv(extra map[string]string) func(string) (string, bool) {
	env := map[string]string{
		"APP_ENV": "production", "PUBLIC_URL": "https://chat.example.com",
		"S3_ACCESS_KEY": "an-access-key", "S3_SECRET_KEY": "a-real-secret-key",
	}
	for k, v := range extra {
		env[k] = v
	}
	return keyEnv(env)
}

func TestKeyRotationSettingsAreOptional(t *testing.T) {
	old, key, oldKey := strings.Repeat("o", 40), strings.Repeat("d", 40), strings.Repeat("p", 40)
	full := map[string]string{"JWT_SECRET_PREVIOUS": old, "DATA_KEY": key, "DATA_KEY_PREVIOUS": oldKey}
	for name, env := range map[string]func(map[string]string) func(string) (string, bool){"development": keyEnv, "production": productionKeyEnv} {
		cfg, err := FromEnv(env(nil))
		if err != nil || cfg.JWTSecretPrevious != "" || cfg.DataKey != "" || cfg.DataKeyPrevious != "" {
			t.Fatalf("%s, an instance that sets nothing new: %v", name, err)
		}
		cfg, err = FromEnv(env(full))
		if err != nil || cfg.JWTSecretPrevious != old || cfg.DataKey != key || cfg.DataKeyPrevious != oldKey {
			t.Fatalf("%s, a full rotation: %v", name, err)
		}
	}
}

func TestKeyRotationSettingsMustBeDistinctKeys(t *testing.T) {
	jwt, old := strings.Repeat("j", 40), strings.Repeat("o", 40)
	key := strings.Repeat("d", 40)
	refused := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"the previous session secret repeats the current", map[string]string{"JWT_SECRET_PREVIOUS": jwt}, "JWT_SECRET_PREVIOUS is the same as JWT_SECRET"},
		{"the previous data key repeats the current", map[string]string{"DATA_KEY": key, "DATA_KEY_PREVIOUS": key}, "DATA_KEY_PREVIOUS is the same as DATA_KEY"},
		{"the data key is the session secret", map[string]string{"DATA_KEY": jwt}, "DATA_KEY must not be a session secret (it equals JWT_SECRET)"},
		{"the data key is the previous session secret", map[string]string{"DATA_KEY": old, "JWT_SECRET_PREVIOUS": old}, "DATA_KEY must not be a session secret (it equals JWT_SECRET_PREVIOUS)"},
		{"the previous data key is the session secret", map[string]string{"DATA_KEY": key, "DATA_KEY_PREVIOUS": jwt}, "DATA_KEY_PREVIOUS must not be a session secret (it equals JWT_SECRET)"},
		{"the previous data key is the previous session secret", map[string]string{"DATA_KEY": key, "DATA_KEY_PREVIOUS": old, "JWT_SECRET_PREVIOUS": old}, "DATA_KEY_PREVIOUS must not be a session secret (it equals JWT_SECRET_PREVIOUS)"},
	}
	for _, tc := range refused {
		for name, env := range map[string]func(map[string]string) func(string) (string, bool){"development": keyEnv, "production": productionKeyEnv} {
			if _, err := FromEnv(env(tc.env)); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("%s, %s: %v", name, tc.name, err)
			}
		}
	}
}

func TestKeyRotationSettingsAreAsStrongAsTheSessionSecretInProduction(t *testing.T) {
	key := strings.Repeat("d", 40)
	weak := []string{"short", strings.Repeat("s", 31), "replace_with_a_secure_random_string_in_production", "change-me"}
	for _, value := range weak {
		for _, tc := range []struct {
			variable string
			env      map[string]string
		}{
			{"JWT_SECRET_PREVIOUS", map[string]string{"JWT_SECRET_PREVIOUS": value}},
			{"DATA_KEY", map[string]string{"DATA_KEY": value}},
			{"DATA_KEY_PREVIOUS", map[string]string{"DATA_KEY": key, "DATA_KEY_PREVIOUS": value}},
			{"DATA_KEY_PREVIOUS", map[string]string{"DATA_KEY_PREVIOUS": value}},
		} {
			_, err := FromEnv(productionKeyEnv(tc.env))
			if err == nil || !strings.Contains(err.Error(), tc.variable+" must be a random string of at least 32 characters in production") {
				t.Errorf("production, %s=%q: %v", tc.variable, value, err)
			}
			// Development relaxes this as it does for JWT_SECRET itself.
			if _, err := FromEnv(keyEnv(tc.env)); err != nil {
				t.Errorf("development, %s=%q: %v", tc.variable, value, err)
			}
		}
	}
}

// The way back from a data key: the old value is named as the previous one
// and DATA_KEY is left empty, so rotate-keys moves everything back under the
// session secret.
func TestAPreviousDataKeyIsAcceptedWithoutADataKey(t *testing.T) {
	oldKey := strings.Repeat("p", 40)
	for name, env := range map[string]func(map[string]string) func(string) (string, bool){"development": keyEnv, "production": productionKeyEnv} {
		cfg, err := FromEnv(env(map[string]string{"DATA_KEY_PREVIOUS": oldKey}))
		if err != nil || cfg.DataKey != "" || cfg.DataKeyPrevious != oldKey {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := FromEnv(keyEnv(map[string]string{"DATA_KEY_PREVIOUS": strings.Repeat("j", 40)})); err == nil ||
		!strings.Contains(err.Error(), "DATA_KEY_PREVIOUS must not be a session secret (it equals JWT_SECRET)") {
		t.Fatalf("a previous data key that is the session secret: %v", err)
	}
}

// The setup wizard judges an .env with RequiredProblems before the stack is
// started: it reports what the start would refuse, all of it at once.
func TestTheSetupPreCheckKnowsTheRotationKeys(t *testing.T) {
	jwt := strings.Repeat("j", 40)
	problems := RequiredProblems(productionKeyEnv(map[string]string{
		"JWT_SECRET_PREVIOUS": jwt, "DATA_KEY": "short", "DATA_KEY_PREVIOUS": "change-me",
	}))
	got := map[string]bool{}
	for _, p := range problems {
		got[p.Var] = true
		if !strings.HasPrefix(p.Text, p.Var+" ") || strings.Contains(p.Text, jwt) {
			t.Errorf("a problem starts with its variable and never prints a value: %q", p.Text)
		}
	}
	if len(problems) != 3 || !got["JWT_SECRET_PREVIOUS"] || !got["DATA_KEY"] || !got["DATA_KEY_PREVIOUS"] {
		t.Fatalf("problems = %+v", problems)
	}
	if ps := RequiredProblems(productionKeyEnv(map[string]string{"DATA_KEY": strings.Repeat("d", 40)})); len(ps) != 0 {
		t.Fatalf("a good data key: %+v", ps)
	}
}
