package bot

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/msch128/mnema-talk/internal/db"
	"github.com/msch128/mnema-talk/internal/httpx"
)

const (
	// MaxCommands bounds the commands of one bot.
	MaxCommands = 50
	// MaxOptions bounds the options of one command.
	MaxOptions = 10
	// MaxOptionValue bounds a string option a member types.
	MaxOptionValue = 1000
)

// Option types, with Discord's numbers.
const (
	OptionString  = 3
	OptionInteger = 4
	OptionBoolean = 5
	OptionUser    = 6
	OptionChannel = 7
)

var nameRE = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// Option is one argument of a command.
type Option struct {
	Name        string `json:"name" minLength:"1" maxLength:"32"`
	Description string `json:"description" minLength:"1" maxLength:"100"`
	// Type is Discord's option type: 3 string, 4 integer, 5 boolean, 6 user
	// (a member's ID), 7 channel (a channel ID).
	Type     int  `json:"type" enums:"3,4,5,6,7"`
	Required bool `json:"required"`
}

// CommandInput is what a bot sends to register a command.
type CommandInput struct {
	// Name is lowercase letters, digits, '-' and '_' (1 to 32).
	Name        string   `json:"name" minLength:"1" maxLength:"32"`
	Description string   `json:"description" minLength:"1" maxLength:"100"`
	Options     []Option `json:"options"`
}

// Command is a registered command.
type Command struct {
	ID          uuid.UUID `json:"id" format:"uuid"`
	BotID       uuid.UUID `json:"bot_id" format:"uuid"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Options     []Option  `json:"options"`
	// Disabled commands are hidden from members and refuse invocations; only
	// an admin can set it, a bot registering the command again keeps it.
	Disabled  bool      `json:"disabled"`
	CreatedAt time.Time `json:"created_at" format:"date-time"`
}

// Clean validates and normalizes a command definition.
func (in CommandInput) Clean() (CommandInput, error) {
	if !nameRE.MatchString(in.Name) {
		return in, httpx.ErrInvalidInput("command names are 1 to 32 lowercase letters, digits, - or _")
	}
	desc, err := httpx.CleanText("description", in.Description, 100, true)
	if err != nil {
		return in, err
	}
	in.Description = desc
	if len(in.Options) > MaxOptions {
		return in, httpx.ErrInvalidInput(fmt.Sprintf("a command has at most %d options", MaxOptions))
	}
	if in.Options == nil {
		in.Options = []Option{}
	}
	if err := cleanOptions(in.Options); err != nil {
		return in, err
	}
	return in, nil
}

func isValidOptionType(t int) bool {
	switch t {
	case OptionString, OptionInteger, OptionBoolean, OptionUser, OptionChannel:
		return true
	default:
		return false
	}
}

func cleanOptionEntry(o *Option, seen map[string]bool) error {
	if !nameRE.MatchString(o.Name) || seen[o.Name] {
		return httpx.ErrInvalidInput("option names are 1 to 32 lowercase letters, digits, - or _, and unique")
	}
	seen[o.Name] = true
	d, err := httpx.CleanText("option description", o.Description, 100, true)
	if err != nil {
		return err
	}
	o.Description = d
	if !isValidOptionType(o.Type) {
		return httpx.ErrInvalidInput("unknown option type")
	}
	return nil
}

// cleanOptions validates option names, descriptions, types and order, and
// normalizes the descriptions in place.
func cleanOptions(opts []Option) error {
	seen := map[string]bool{}
	optional := false
	for i := range opts {
		if err := cleanOptionEntry(&opts[i], seen); err != nil {
			return err
		}
		// As in Discord, required options come first.
		if opts[i].Required && optional {
			return httpx.ErrInvalidInput("required options must come before optional ones")
		}
		optional = optional || !opts[i].Required
	}
	return nil
}

const commandColumns = `id, bot_id, name, description, options, disabled, created_at`

func scanCommand(row pgx.Row) (Command, error) {
	var c Command
	// options is a JSON array (a check constraint), scanned straight into the slice.
	err := row.Scan(&c.ID, &c.BotID, &c.Name, &c.Description, &c.Options, &c.Disabled, &c.CreatedAt)
	return c, err
}

// ListCommands lists the commands of a bot by name.
func ListCommands(ctx context.Context, q db.Querier, botID uuid.UUID) ([]Command, error) {
	list := []Command{}
	rows, err := q.Query(ctx, `SELECT `+commandColumns+` FROM bot_commands WHERE bot_id = $1 ORDER BY name`, botID)
	if err == nil {
		list, err = pgx.CollectRows(rows, func(row pgx.CollectableRow) (Command, error) { return scanCommand(row) })
	}
	if err != nil {
		return nil, fmt.Errorf("list commands: %w", err)
	}
	return list, nil
}

func upsert(ctx context.Context, q db.Querier, botID uuid.UUID, in CommandInput) (Command, error) {
	opts, err := json.Marshal(in.Options)
	if err != nil {
		return Command{}, fmt.Errorf("marshal options: %w", err)
	}
	return scanCommand(q.QueryRow(ctx, `
		INSERT INTO bot_commands (bot_id, name, description, options) VALUES ($1, $2, $3, $4)
		ON CONFLICT (bot_id, name) DO UPDATE SET description = EXCLUDED.description, options = EXCLUDED.options, updated_at = NOW()
		RETURNING `+commandColumns, botID, in.Name, in.Description, opts))
}

// UpsertCommand registers or updates a command by name, keeping an admin's
// disabled flag.
func UpsertCommand(ctx context.Context, p *db.Pool, botID uuid.UUID, in CommandInput) (Command, error) {
	var out Command
	err := pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		if err := lockBot(ctx, tx, botID); err != nil {
			return err
		}
		var n int
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT COUNT(*), COALESCE(BOOL_OR(name = $2), FALSE) FROM bot_commands WHERE bot_id = $1`, botID, in.Name).Scan(&n, &exists); err != nil {
			return err
		}
		if !exists && n >= MaxCommands {
			return httpx.ErrConflict(fmt.Sprintf("a bot can have at most %d commands", MaxCommands))
		}
		var err error
		out, err = upsert(ctx, tx, botID, in)
		return err
	})
	return out, wrapDB(err, "register command")
}

func collectUniqueCommandNames(ins []CommandInput) ([]string, error) {
	if len(ins) > MaxCommands {
		return nil, httpx.ErrInvalidInput(fmt.Sprintf("a bot can have at most %d commands", MaxCommands))
	}
	names := make([]string, 0, len(ins))
	seen := map[string]bool{}
	for _, in := range ins {
		if seen[in.Name] {
			return nil, httpx.ErrInvalidInput("duplicate command name: " + in.Name)
		}
		seen[in.Name] = true
		names = append(names, in.Name)
	}
	return names, nil
}

func replaceCommandsTx(ctx context.Context, tx pgx.Tx, botID uuid.UUID, names []string, ins []CommandInput) error {
	if err := lockBot(ctx, tx, botID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM bot_commands WHERE bot_id = $1 AND NOT (name = ANY($2))`, botID, names); err != nil {
		return err
	}
	for _, in := range ins {
		if _, err := upsert(ctx, tx, botID, in); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceCommands makes the given definitions the bot's commands: it adds and
// updates by name and removes the others (Discord's bulk overwrite).
func ReplaceCommands(ctx context.Context, p *db.Pool, botID uuid.UUID, ins []CommandInput) ([]Command, error) {
	names, err := collectUniqueCommandNames(ins)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, p, func(tx pgx.Tx) error {
		return replaceCommandsTx(ctx, tx, botID, names, ins)
	})
	if err != nil {
		return nil, wrapDB(err, "replace commands")
	}
	return ListCommands(ctx, p, botID)
}

func lockBot(ctx context.Context, tx pgx.Tx, botID uuid.UUID) error {
	var one int
	err := tx.QueryRow(ctx, `SELECT 1 FROM bots WHERE id = $1 FOR UPDATE`, botID).Scan(&one)
	if errors.Is(err, pgx.ErrNoRows) {
		return errNotFound
	}
	return err
}

func wrapDB(err error, what string) error {
	if err == nil {
		return nil
	}
	if _, ok := httpx.AsAPIError(err); ok {
		return err
	}
	return fmt.Errorf("%s: %w", what, err)
}

var errCommandNotFound = httpx.ErrNotFound("command not found")

// DeleteCommand removes one command.
func DeleteCommand(ctx context.Context, q db.Querier, botID, id uuid.UUID) error {
	tag, err := q.Exec(ctx, `DELETE FROM bot_commands WHERE id = $1 AND bot_id = $2`, id, botID)
	if err != nil {
		return fmt.Errorf("delete command: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return errCommandNotFound
	}
	return nil
}

// SetCommandDisabled switches a command off or on (an admin's decision).
func SetCommandDisabled(ctx context.Context, q db.Querier, botID, id uuid.UUID, disabled bool) (Command, error) {
	c, err := scanCommand(q.QueryRow(ctx, `
		UPDATE bot_commands SET disabled = $3, updated_at = NOW() WHERE id = $1 AND bot_id = $2 RETURNING `+commandColumns, id, botID, disabled))
	if errors.Is(err, pgx.ErrNoRows) {
		return c, errCommandNotFound
	}
	if err != nil {
		return c, fmt.Errorf("update command: %w", err)
	}
	return c, nil
}

// getCommand loads one command with its bot's account and state, for an invocation.
func getCommand(ctx context.Context, q db.Querier, id uuid.UUID) (Command, uuid.UUID, bool, error) {
	var userID uuid.UUID
	var botDisabled bool
	var c Command
	err := q.QueryRow(ctx, `
		SELECT c.id, c.bot_id, c.name, c.description, c.options, c.disabled, c.created_at, b.user_id, b.disabled
		FROM bot_commands c JOIN bots b ON b.id = c.bot_id WHERE c.id = $1`, id).Scan(
		&c.ID, &c.BotID, &c.Name, &c.Description, &c.Options, &c.Disabled, &c.CreatedAt, &userID, &botDisabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, uuid.Nil, false, errCommandNotFound
	}
	if err != nil {
		return c, uuid.Nil, false, fmt.Errorf("load command: %w", err)
	}
	return c, userID, botDisabled, nil
}
