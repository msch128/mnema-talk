package bot

import "testing"

func TestUsernameBaseIsTheReadableNameOfTheBot(t *testing.T) {
	cases := map[string]string{
		"Mnemo":                                "mnemo",
		"Mnema Bot":                            "mnema_bot",
		"  --Deploy.Helper!  ":                 "deploy_helper",
		"Caf\u00e9 Bot":                        "caf_bot",
		"a":                                    "",
		"!!":                                   "",
		"日本語":                                  "",
		"x1":                                   "x1",
		"abcdefghijklmnopqrstuvwx_":            "abcdefghijklmnopqrstuvwx",
		"abcdefghijklmnopqrstuvwxyz0123456789": "abcdefghijklmnopqrstuvwxyz01",
	}
	for in, want := range cases {
		if got := usernameBase(in); got != want {
			t.Errorf("usernameBase(%q) = %q, want %q", in, got, want)
		}
	}
}
