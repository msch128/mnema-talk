package emoji

import "github.com/msch128/mnema-talk/internal/userdata"

// accountData is the custom emoji an account uploaded in an account erasure. It exports nothing: the
// rows only name the account as their creator.
type accountData struct{}

// RegisterAccountData adds custom emoji's erasure statements.
func RegisterAccountData(r *userdata.Registry) { r.Register(nil, accountData{}) }

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderEmoji, SQL: `UPDATE custom_emoji SET created_by = NULL WHERE created_by = $1`},
	}
}
