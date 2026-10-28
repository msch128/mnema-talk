package httpx

// Helpers shared by the in-memory limiters (RateLimiter, LoginFailureLimiter):
// each keeps a map of per-key state that must stay bounded. The callers hold
// their own mutex and decide what "expired" and "worst victim" mean.

// clearMatching deletes every key for which match reports true and returns how
// many it removed.
func clearMatching[V any](m map[string]V, match func(key string) bool) int {
	n := 0
	for k := range m {
		if match(k) {
			delete(m, k)
			n++
		}
	}
	return n
}

// dropWhere deletes every entry for which drop reports true.
func dropWhere[V any](m map[string]V, drop func(key string, v V) bool) {
	for k, v := range m {
		if drop(k, v) {
			delete(m, k)
		}
	}
}

// evictSampled deletes one entry: among a sample of evictSample entries it
// picks the one whose score is preferred by better(candidate, current victim).
func evictSampled[V, S any](m map[string]V, score func(key string, v V) S, better func(candidate, victim S) bool) {
	var victim string
	var victimScore S
	n := 0
	for k, v := range m {
		if s := score(k, v); n == 0 || better(s, victimScore) {
			victim, victimScore = k, s
		}
		if n++; n >= evictSample {
			break
		}
	}
	if n > 0 {
		delete(m, victim)
	}
}
