package config

type namedKey struct{ name, value string }

func (k namedKey) problem(text string) Problem {
	return Problem{Var: k.name, Text: k.name + " " + text}
}

// rotationKeyProblems checks the optional keys of a rotation (see "Rotating
// keys" in doc/operations.md). A mistake here would not show at once: the
// server would start and sign-ins with a second factor would fail later.
//
// DATA_KEY_PREVIOUS without DATA_KEY is allowed: it is the way back from a
// data key to the key derived from the session secret.
func rotationKeyProblems(r *envReader, production bool) []Problem {
	read := func(name string) namedKey { return namedKey{name, r.get(name, "")} }
	session := []namedKey{read("JWT_SECRET"), read("JWT_SECRET_PREVIOUS")}
	data := []namedKey{read("DATA_KEY"), read("DATA_KEY_PREVIOUS")}
	var ps []Problem
	for _, pair := range [][]namedKey{session, data} {
		if pair[1].value != "" && pair[1].value == pair[0].value {
			ps = append(ps, pair[1].problem("is the same as "+pair[0].name+": it takes the value you are replacing, and "+pair[0].name+" the new one."))
		}
	}
	ps = append(ps, sessionSecretsAsDataKeys(data, session)...)
	if !production {
		return ps
	}
	// The same strength as JWT_SECRET, which productionProblems checks.
	for _, k := range []namedKey{session[1], data[0], data[1]} {
		if k.value != "" && (len(k.value) < 32 || isPlaceholder(k.value)) {
			ps = append(ps, k.problem("must be a random string of at least 32 characters in production when set, e.g. the output of `openssl rand -hex 32`."))
		}
	}
	return ps
}

// sessionSecretsAsDataKeys refuses a data key that is a session secret: the
// two are separate so that either can be replaced on its own.
func sessionSecretsAsDataKeys(data, session []namedKey) []Problem {
	var ps []Problem
	for _, d := range data {
		for _, s := range session {
			if d.value != "" && d.value == s.value {
				ps = append(ps, d.problem("must not be a session secret (it equals "+s.name+"): a data key has its own value, e.g. the output of `openssl rand -hex 32`. What is sealed under the session secret stays readable without naming it."))
			}
		}
	}
	return ps
}
