package auth

import "github.com/msch128/mnema-talk/internal/userdata"

// personalFallback is the registry integration tests install. Those tests
// build a Handler without the server. Production leaves it nil.
var personalFallback *userdata.Registry

// SetPersonalData registers the domains that contribute export files and
// erasure statements. The server calls it once, before it serves.
func (h *Handler) SetPersonalData(r *userdata.Registry) {
	h.personal = r
}

func (h *Handler) personalData() *userdata.Registry {
	if h != nil && h.personal != nil {
		return h.personal
	}
	return personalFallback
}

// ErasePlan is every erasure statement in the order it runs. The anonymise of
// the account row follows the plan and is not part of it.
func (h *Handler) ErasePlan() []userdata.Step {
	return mergedEraseSteps(h.personalData())
}
