package audit

import "testing"

// The classification is a policy, so it is pinned action by action: a change
// here is a decision about who may read what, not a refactoring.
func TestClassesPinTheVisibilityOfEveryAction(t *testing.T) {
	want := map[string]Visibility{
		// Events written by handlers.
		"message.delete": VisChannel, "message.edit": VisChannel, "message.pin": VisChannel, "message.unpin": VisChannel,
		"moderation.bulk_delete": VisChannel,
		"voice.join":             VisChannel, "voice.leave": VisChannel,
		"auth.login": VisPrivileged, "auth.login_failed": VisPrivileged, "auth.login_locked": VisPrivileged,
		"auth.register": VisPrivileged, "auth.password_change": VisPrivileged, "auth.logout_all": VisPrivileged,
		"auth.email_set": VisPrivileged, "auth.email_remove": VisPrivileged, "auth.email_verified": VisPrivileged, "auth.password_reset_mail": VisPrivileged,
		// Admin routes.
		"invite.create": VisAuditors, "invite.revoke": VisAuditors,
		"user.disable": VisAuditors, "user.enable": VisAuditors, "user.sessions_revoke": VisAuditors,
		"user.status": VisAuditors, "user.talk_kick": VisAuditors, "user.roles_set": VisAuditors,
		"user.password_set": VisPrivileged, "user.password_reset": VisPrivileged, "user.lockout_clear": VisPrivileged,
		"role.create": VisAuditors, "role.update": VisAuditors, "role.delete": VisAuditors, "role.reorder": VisAuditors,
		"category.overwrites_set": VisPrivileged,
		"channel.overwrites_set":  VisChannel, "channel.create": VisChannel, "channel.update": VisChannel, "channel.delete": VisChannel, "channel.duplicate": VisChannel,
		"category.create": VisPrivileged, "category.update": VisPrivileged, "category.delete": VisPrivileged, "layout.apply": VisPrivileged,
		"bot.create": VisPrivileged, "bot.update": VisPrivileged, "bot.delete": VisPrivileged, "bot.avatar": VisPrivileged, "bot.roles": VisPrivileged,
		"bot.token_create": VisPrivileged, "bot.token_regenerate": VisPrivileged, "bot.token_revoke": VisPrivileged,
		"bot.command_update": VisPrivileged, "bot.command_delete": VisPrivileged,
		"webhook.create": VisPrivileged, "webhook.update": VisPrivileged, "webhook.regenerate": VisPrivileged,
		"webhook.avatar": VisPrivileged, "webhook.delete": VisPrivileged,
		"outgoing_webhook.create": VisPrivileged, "outgoing_webhook.update": VisPrivileged,
		"outgoing_webhook.rotate_secret": VisPrivileged, "outgoing_webhook.delete": VisPrivileged,
		"media.prune": VisPrivileged, "media.orphans_cleanup": VisPrivileged, "media.delete": VisPrivileged,
		"settings.upload_limit": VisPrivileged, "settings.onboarding": VisPrivileged, "system.update": VisPrivileged, "system.mail_test": VisPrivileged,
		"moderation.timeout": VisAuditors, "moderation.timeout_remove": VisAuditors, "moderation.ban": VisAuditors,
		"moderation.unban": VisAuditors, "moderation.ban_expired": VisAuditors,
		"admin.post": VisPrivileged,
	}
	for _, rt := range Routes {
		if rt.Skip {
			continue
		}
		if _, ok := want[rt.Action]; !ok {
			t.Errorf("route action %s has no pinned visibility", rt.Action)
		}
	}
	for action, vis := range want {
		if got := ClassOf(action); got != vis {
			t.Errorf("%s: class %d, want %d", action, got, vis)
		}
	}
	if ClassOf("something.new") != VisAuditors {
		t.Error("unknown actions default to the auditors")
	}
	if matchLike("a.%", "a") || !matchLike("a.%", "a.b") || matchLike("exact", "exact2") || !matchLike("%", "x") {
		t.Error("matchLike")
	}
}

// An auditor without Administrator must not see categories: the filter hides
// every pattern of the privileged class.
func TestCategoryAndLayoutEntriesAreHiddenFromAuditors(t *testing.T) {
	hidden := patterns(VisPrivileged)
	for _, want := range []string{"category.%", "layout.apply"} {
		found := false
		for _, p := range hidden {
			found = found || p == want
		}
		if !found {
			t.Errorf("%s is not hidden from auditors: %v", want, hidden)
		}
	}
	for _, a := range []string{"category.create", "category.overwrites_set", "layout.apply"} {
		if ClassOf(a) != VisPrivileged {
			t.Errorf("%s is %v", a, ClassOf(a))
		}
	}
}
