package webhook

import "github.com/msch128/mnema-talk/internal/userdata"

// accountData is the webhooks an account created in an account erasure. It exports nothing: the
// rows only name the account as their creator.
type accountData struct{}

// RegisterAccountData adds webhooks' erasure statements.
func RegisterAccountData(r *userdata.Registry) { r.Register(nil, accountData{}) }

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderWebhooks, SQL: `UPDATE webhooks SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderOutgoingWebhooks, SQL: `UPDATE outgoing_webhooks SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderOutgoingWebhookURL, SQL: `UPDATE outgoing_webhooks SET url_set_by = NULL WHERE url_set_by = $1`},
	}
}
