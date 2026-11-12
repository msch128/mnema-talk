package audit

import (
	"strings"
	"testing"
)

// The classification is a policy, so it is pinned action by action: a change
// here is a decision about who may read what, not a refactoring.
func TestClassesPinTheVisibilityOfEveryAction(t *testing.T) {
	want := map[string]Visibility{
		// Events written by handlers.
		"message.delete": VisChannel, "message.edit": VisChannel, "message.pin": VisChannel, "message.unpin": VisChannel,
		"message.edit_history_view": VisChannel, "poll.end": VisChannel,
		"moderation.bulk_delete": VisChannel,
		"voice.join":             VisChannel, "voice.leave": VisChannel,
		"auth.login": VisPrivileged, "auth.login_failed": VisPrivileged, "auth.login_locked": VisPrivileged,
		"auth.register": VisPrivileged, "auth.password_change": VisPrivileged, "auth.logout_all": VisPrivileged, "auth.session_revoke": VisPrivileged, "auth.sessions_revoke_others": VisPrivileged,
		"auth.two_factor_enabled": VisPrivileged, "auth.two_factor_disabled": VisPrivileged, "auth.two_factor_failed": VisPrivileged, "auth.two_factor_alert": VisPrivileged,
		"auth.recovery_code_used": VisPrivileged, "auth.account_delete": VisPrivileged, "auth.export_request": VisPrivileged, "auth.export_download": VisPrivileged, "auth.recovery_codes_regenerated": VisPrivileged, "user.two_factor_reset": VisPrivileged,
		"auth.email_set": VisPrivileged, "auth.email_remove": VisPrivileged, "auth.email_verified": VisPrivileged, "auth.password_reset_mail": VisPrivileged,
		// Admin routes.
		"invite.create": VisAuditors, "invite.vanity_link": VisAuditors, "invite.update": VisAuditors, "invite.revoke": VisAuditors,
		"user.disable": VisAuditors, "user.enable": VisAuditors, "user.sessions_revoke": VisAuditors,
		"user.status": VisAuditors, "user.talk_kick": VisAuditors, "user.roles_set": VisAuditors,
		"user.password_set": VisPrivileged, "user.password_reset": VisPrivileged, "user.lockout_clear": VisPrivileged,
		"role.create": VisAuditors, "role.update": VisAuditors, "role.delete": VisAuditors, "role.reorder": VisAuditors,
		// A member choosing a role for themselves is administration news, not private content.
		"role.self_assign": VisAuditors, "role.self_remove": VisAuditors,
		"category.overwrites_set": VisPrivileged,
		"channel.overwrites_set":  VisChannel, "channel.create": VisChannel, "channel.update": VisChannel, "channel.delete": VisChannel, "channel.duplicate": VisChannel, "channel.auto_delete_set": VisChannel,
		"category.create": VisPrivileged, "category.update": VisPrivileged, "category.delete": VisPrivileged, "layout.apply": VisPrivileged,
		"bot.create": VisPrivileged, "bot.update": VisPrivileged, "bot.delete": VisPrivileged, "bot.avatar": VisPrivileged, "bot.roles": VisPrivileged,
		"bot.token_create": VisPrivileged, "bot.token_regenerate": VisPrivileged, "bot.token_revoke": VisPrivileged,
		"bot.command_update": VisPrivileged, "bot.command_delete": VisPrivileged,
		"webhook.create": VisPrivileged, "webhook.update": VisPrivileged, "webhook.regenerate": VisPrivileged,
		"webhook.avatar": VisPrivileged, "webhook.delete": VisPrivileged,
		"outgoing_webhook.create": VisPrivileged, "outgoing_webhook.update": VisPrivileged,
		"outgoing_webhook.rotate_secret": VisPrivileged, "outgoing_webhook.delete": VisPrivileged,
		"media.prune": VisPrivileged, "media.orphans_cleanup": VisPrivileged, "media.delete": VisPrivileged,
		"import.discord": VisPrivileged, "import.discord_undo": VisPrivileged,
		"settings.upload_limit": VisPrivileged, "settings.onboarding": VisPrivileged, "settings.raid": VisPrivileged, "settings.lockdown": VisPrivileged, "settings.community_icon": VisPrivileged, "settings.community_icon_remove": VisPrivileged, "system.update": VisPrivileged, "system.mail_test": VisPrivileged,
		"moderation.timeout": VisAuditors, "moderation.timeout_remove": VisAuditors, "moderation.ban": VisAuditors,
		"moderation.unban": VisAuditors, "moderation.ban_expired": VisAuditors,
		"emoji.create": VisAuditors, "emoji.rename": VisAuditors, "emoji.delete": VisAuditors,
		"automod.rule_create": VisAuditors, "automod.rule_update": VisAuditors, "automod.rule_delete": VisAuditors,
		"automod.timeout": VisAuditors, "automod.raid": VisAuditors,
		"moderation.note_add": VisPrivileged, "moderation.note_edit": VisPrivileged, "moderation.note_delete": VisPrivileged,
		"event.create": VisAuditors, "event.edit": VisAuditors, "event.cancel": VisAuditors,
		"event.talk_create": VisChannel, "event.talk_edit": VisChannel, "event.talk_cancel": VisChannel,
		"report.create": VisPrivileged, "report.resolve": VisPrivileged, "report.delete": VisPrivileged,
		"admin.post": VisPrivileged,
		// Written by handlers outside the admin area; found by the source scan.
		"forum.post_update": VisChannel, "forum.settings_update": VisChannel, "forum.tag_create": VisChannel,
		"forum.tag_update": VisChannel, "forum.tag_delete": VisChannel, "forum.tag_reorder": VisChannel,
		"channel.auto_delete_run": VisChannel, "event.talk_comment_delete": VisChannel,
		"moderation.kick": VisAuditors, "event.comment_delete": VisAuditors,
		"event.role_template_create": VisAuditors, "event.role_template_update": VisAuditors, "event.role_template_delete": VisAuditors,
		"wiki.create": VisAuditors, "wiki.edit": VisAuditors, "wiki.move": VisAuditors, "wiki.delete": VisAuditors,
		"wiki.restore": VisAuditors, "wiki.lock": VisAuditors, "wiki.image": VisAuditors,
		"media.avatar_removed": VisPrivileged,
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
	// A forgotten action fails closed: only the Owner and Administrators read it.
	for _, a := range []string{"something.new", "user.something_new", "wiki.something_new", "forum"} {
		if ClassOf(a) != VisPrivileged {
			t.Errorf("unknown action %s is not privileged", a)
		}
	}
	if matchLike("a.%", "a") || !matchLike("a.%", "a.b") || matchLike("exact", "exact2") || !matchLike("%", "x") {
		t.Error("matchLike")
	}
}

// An auditor without Administrator must not see categories: the table has
// privileged rows for them, which the filter hides.
func TestCategoryAndLayoutEntriesAreHiddenFromAuditors(t *testing.T) {
	for _, want := range []string{"category.%", "layout.apply"} {
		found := false
		for _, c := range Classes {
			found = found || (c.Pattern == want && c.Vis == VisPrivileged)
		}
		if !found {
			t.Errorf("%s is not hidden from auditors", want)
		}
	}
	for _, a := range []string{"category.create", "category.overwrites_set", "layout.apply"} {
		if ClassOf(a) != VisPrivileged {
			t.Errorf("%s is %v", a, ClassOf(a))
		}
	}
}

// Every action the server can write has a row of its own: the catch-all is
// the narrowest class, so an action left to it would be hidden from the
// auditors who should read it, and before it was the narrowest it showed the
// forum entries of private forums to every auditor (H-L6-01). The actions are
// found in the source, not listed by hand, so a new writer is seen.
func TestEveryWrittenActionHasAClassOfItsOwn(t *testing.T) {
	s := newActionScan(t, "../../internal", "../../cmd")
	s.run()
	for _, e := range s.errs {
		t.Error(e)
	}
	if len(s.found) < 100 {
		t.Fatalf("the scan found only %d actions: it no longer sees the writers", len(s.found))
	}
	for action, at := range s.found {
		if row := ownRow(action); row == nil {
			t.Errorf("%s (written at %s) has no row of its own in Classes: decide who may read it", action, at)
		}
	}
	for _, want := range []string{"forum.post_update", "forum.tag_create", "user.two_factor_reset", "invite.create", "moderation.kick", "event.talk_*"} {
		if _, ok := s.found[want]; !ok {
			t.Errorf("the scan misses %s", want)
		}
	}
}

// ownRow is the first row covering action other than the catch-all. A scanned
// value ending in * stands for every action with that prefix: only a prefix
// row that covers all of them counts, and no earlier row may match some.
func ownRow(action string) *Class {
	prefix, open := strings.CutSuffix(action, "*")
	for i, c := range Classes {
		if c.Pattern == "%" {
			return nil
		}
		base, isPrefix := strings.CutSuffix(c.Pattern, "%")
		switch {
		case !open && matchLike(c.Pattern, action):
			return &Classes[i]
		case open && isPrefix && strings.HasPrefix(prefix, base):
			return &Classes[i]
		case open && strings.HasPrefix(base, prefix):
			return nil
		}
	}
	return nil
}
