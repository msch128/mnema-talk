package auth

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// defaultMailLocale is the instance default: the language of a mail when the
// account has no stored language and the request names none we ship.
const defaultMailLocale = "en"

// mailLocale is the one rule every account mail follows (verification, reset,
// security alert): the member's stored UI language if there is one, otherwise
// the first language of the triggering request's Accept-Language header that
// the app ships, otherwise the instance default. acceptLanguage is "" when no
// request of the member triggered the mail.
func mailLocale(stored, acceptLanguage string) string {
	if SupportedLocales[stored] {
		return stored
	}
	if l := preferredLocale(acceptLanguage); l != "" {
		return l
	}
	return defaultMailLocale
}

// requestLanguage is the Accept-Language header of r, to be handed to mailLocale.
func requestLanguage(r *http.Request) string { return r.Header.Get("Accept-Language") }

// preferredLocale returns the supported language the header ranks highest, or "".
func preferredLocale(header string) string {
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
		if q > 0 && SupportedLocales[primary] {
			choices = append(choices, choice{primary, q})
		}
	}
	sort.SliceStable(choices, func(i, j int) bool { return choices[i].q > choices[j].q })
	if len(choices) == 0 {
		return ""
	}
	return choices[0].lang
}
