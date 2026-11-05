package discordimport

import "github.com/msch128/mnema-talk/internal/userdata"

// accountData is the Discord import history in an account erasure. It exports nothing: the
// rows only name the account as their creator.
type accountData struct{}

// RegisterAccountData adds the import history's erasure statements.
func RegisterAccountData(r *userdata.Registry) { r.Register(nil, accountData{}) }

func (accountData) Erase() []userdata.Step {
	return []userdata.Step{
		{Order: userdata.OrderDiscordImports, SQL: `UPDATE discord_imports SET created_by_name = '' WHERE created_by = $1`},
		{Order: userdata.OrderDiscordImportCreatedBy, SQL: `UPDATE discord_imports SET created_by = NULL WHERE created_by = $1`},
		{Order: userdata.OrderDiscordImportUndoneBy, SQL: `UPDATE discord_imports SET undone_by = NULL WHERE undone_by = $1`},
	}
}
