package bot

import "github.com/msch128/mnema-talk/internal/userdata"

// accountData is the bots and bot tokens an account created in an account erasure. It exports nothing: the
// rows only name the account as their creator.
type accountData struct{}

// RegisterAccountData adds bots' erasure statements.
func RegisterAccountData(r *userdata.Registry) { r.Register(nil, accountData{}) }

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderBotTokens, SQL: `DELETE FROM bot_tokens WHERE created_by = $1`},
		{Order: userdata.OrderBots, SQL: `UPDATE bots SET created_by = NULL WHERE created_by = $1`},
	}
}
