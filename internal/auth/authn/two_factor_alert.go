package authn

import (
	"context"

	"github.com/msch128/mnema-talk/internal/auth"
)

// raiseAlert audits the failure streak and, when the account has a confirmed
// address and the server can send mail, tells its owner. It runs once per
// streak, when the count reaches auth.NoticeAfter. Neither the log nor the mail
// holds a code.
func (h *Handler) raiseAlert(ctx context.Context, u *auth.User) {
	h.AuditUser(ctx, u, "auth.two_factor_alert")
	if !h.mailEnabled() {
		return
	}
	h.background(func(bg context.Context) {
		st, err := loadEmailState(bg, h.Sessions.DB, u.ID)
		if err != nil || st.Address == "" || !st.Verified {
			return
		}
		_ = h.sendMail(bg, h.alertMail(bg, st.Address, mailLocale(st.Locale, ""), st.Username))
	})
}
