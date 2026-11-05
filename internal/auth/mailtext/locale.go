package mailtext

import (
	"sort"
	"strconv"
	"strings"
)

// DefaultLocale is the instance default: the language of a mail when the
// account has no stored language and the request names none we ship.
const DefaultLocale = "en"

// Locale is the one rule every account mail follows (verification, reset,
// security alert): the member's stored UI language if there is one, otherwise
// the first language of the triggering request's Accept-Language header that
// the app ships, otherwise the instance default. acceptLanguage is "" when no
// request of the member triggered the mail. supported is the set of UI
// languages the app ships (auth.SupportedLocales).
func Locale(stored, acceptLanguage string, supported map[string]bool) string {
	if supported[stored] {
		return stored
	}
	if l := preferredLocale(acceptLanguage, supported); l != "" {
		return l
	}
	return DefaultLocale
}

// preferredLocale returns the supported language the header ranks highest, or "".
func preferredLocale(header string, supported map[string]bool) string {
	type choice struct {
		lang string
		q    float64
	}
	var choices []choice
	for _, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		q := 1.0
		if v, ok := strings.CutPrefix(strings.TrimSpace(params), "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err == nil {
				q = f
			}
		}
		primary, _, _ := strings.Cut(strings.ToLower(strings.TrimSpace(tag)), "-")
		if q > 0 && supported[primary] {
			choices = append(choices, choice{primary, q})
		}
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].q > choices[j].q })
	if len(choices) == 0 {
		return ""
	}
	return choices[0].lang
}
