package config

import (
	"net/url"
	"strings"
)

// Problem is one setting that stops the server from starting: what is wrong,
// why the setting matters and an example, in one sentence.
type Problem struct {
	// Var names the variable; empty for a problem that spans several.
	Var string
	// Text is the whole line, starting with the variable name.
	Text string
}

// ConfigError lists every required setting that is missing or invalid, so a
// start fails once with the complete list instead of one variable at a time.
type ConfigError struct{ Problems []Problem }

// ExitCode is the process exit status of a start refused for its settings
// (EX_CONFIG in sysexits.h), distinct from a crash.
const ExitCode = 78

func (e *ConfigError) Error() string {
	var b strings.Builder
	b.WriteString("Mnema Talk cannot start - fix these settings in .env:")
	for _, p := range e.Problems {
		b.WriteString("\n  - " + p.Text)
	}
	return b.String()
}

// RequiredProblems checks the settings a start needs in the given environment
// and returns every problem found, not only the first. An empty list means
// the required settings are present; FromEnv still checks the rest.
func RequiredProblems(lookup func(string) (string, bool)) []Problem {
	r := &envReader{lookup: lookup}
	production := false
	switch strings.ToLower(r.get("APP_ENV", "development")) {
	case "production", "prod", "staging":
		production = true
	}
	var ps []Problem
	add := func(v, text string) { ps = append(ps, Problem{Var: v, Text: v + " " + text}) }

	if r.get("DATABASE_URL", "") == "" {
		add("DATABASE_URL", "is required: the database connection. With the docker-compose.yml it is built from POSTGRES_PASSWORD in .env, so set that (a random password, e.g. the output of `openssl rand -hex 16`).")
	} else if pw := databasePassword(r.get("DATABASE_URL", "")); production && isPlaceholder(pw) {
		add("DATABASE_URL", "still uses the public example password; set POSTGRES_PASSWORD to your own, e.g. the output of `openssl rand -hex 16`.")
	}
	if raw := normalizeURL(r.get("PUBLIC_URL", "http://localhost:8080")); !validPublicURL(raw) {
		add("PUBLIC_URL", "must be the address members open, with http:// or https://, e.g. https://chat.example.com (got \""+raw+"\"). Invite links, e-mails and the browser's origin checks use it.")
	}
	if isPlaceholder(r.get("ADMIN_INITIAL_PASSWORD", "")) {
		add("ADMIN_INITIAL_PASSWORD", "still uses the public example value; set your own or leave it empty to generate one.")
	}
	if production {
		ps = append(ps, productionProblems(r)...)
	}
	return ps
}

func validPublicURL(raw string) bool {
	_, ok := parseAbsoluteHTTP(raw)
	return ok
}

func databasePassword(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.User == nil {
		return ""
	}
	pw, _ := u.User.Password()
	return pw
}

// productionProblems are the secrets a production-like start refuses to run
// without.
func productionProblems(r *envReader) []Problem {
	var ps []Problem
	add := func(v, text string) { ps = append(ps, Problem{Var: v, Text: v + " " + text}) }
	if jwt := r.get("JWT_SECRET", ""); len(jwt) < 32 || isPlaceholder(jwt) {
		add("JWT_SECRET", "must be set to a random string of at least 32 characters in production: it signs login sessions. Example: the output of `openssl rand -hex 32`.")
	}
	for _, v := range []string{"S3_ACCESS_KEY", "S3_SECRET_KEY"} {
		switch val := r.get(v, ""); {
		case val == "":
			add(v, "is required in production: the object storage credentials for uploads and avatars. Example: the output of `openssl rand -hex 16`.")
		case isPlaceholder(val):
			add(v, "still uses the public example value; set your own, e.g. the output of `openssl rand -hex 16`.")
		}
	}
	for _, v := range []string{"METRICS_TOKEN", "WEBRTC_TURN_SECRET"} {
		if isPlaceholder(r.get(v, "")) {
			add(v, "still uses a public example value; set your own or remove it.")
		}
	}
	return ps
}

// ComposeEnv is the environment the app container gets from a .env file
// through docker-compose.yml: the compose defaults for APP_ENV, PUBLIC_URL and
// the S3 access key, and DATABASE_URL built from POSTGRES_PASSWORD. It lets
// RequiredProblems judge an .env before the stack is started.
func ComposeEnv(dotenv map[string]string) func(string) (string, bool) {
	env := map[string]string{"APP_ENV": "production"}
	for k, v := range dotenv {
		if strings.TrimSpace(v) != "" {
			env[k] = v
		}
	}
	if _, ok := env["S3_ACCESS_KEY"]; !ok {
		env["S3_ACCESS_KEY"] = "mnema_admin"
	}
	if pw := env["POSTGRES_PASSWORD"]; pw != "" {
		user, db := env["POSTGRES_USER"], env["POSTGRES_DB"]
		if user == "" {
			user = "mnema"
		}
		if db == "" {
			db = "mnema_talk"
		}
		u := url.URL{Scheme: "postgres", User: url.UserPassword(user, pw), Host: "postgres:5432", Path: "/" + db}
		env["DATABASE_URL"] = u.String()
	}
	return func(k string) (string, bool) { v, ok := env[k]; return v, ok }
}
