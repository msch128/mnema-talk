package db

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

type Pool struct {
	*pgxpool.Pool
}

func Connect(ctx context.Context, databaseURL string) (*Pool, error) {
	config, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	config.MaxConns = 25
	config.MinConns = 5
	config.MaxConnLifetime = 1 * time.Hour
	config.MaxConnIdleTime = 15 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("unable to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	log.Println("[DB] Successfully connected to PostgreSQL 17")
	return &Pool{pool}, nil
}

// Migrate runs the SQL schema migrations
func (p *Pool) Migrate(ctx context.Context, migrationFilePath string) error {
	content, err := os.ReadFile(migrationFilePath)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", migrationFilePath, err)
	}

	_, err = p.Exec(ctx, string(content))
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	// Apply incremental schema changes if table already existed
	incrementalMigrations := `
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS parent_id UUID REFERENCES messages(id) ON DELETE CASCADE;
		ALTER TABLE messages ADD COLUMN IF NOT EXISTS is_edited BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE users ADD COLUMN IF NOT EXISTS bio TEXT NOT NULL DEFAULT '';
		CREATE INDEX IF NOT EXISTS idx_messages_parent_id ON messages(parent_id);
		CREATE TABLE IF NOT EXISTS message_reactions (
			id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			message_id UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
			user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			emoji VARCHAR(32) NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(message_id, user_id, emoji)
		);
		CREATE INDEX IF NOT EXISTS idx_reactions_message ON message_reactions(message_id);

		ALTER TABLE channels DROP CONSTRAINT IF EXISTS channels_type_check;
		ALTER TABLE channels ADD CONSTRAINT channels_type_check CHECK (type IN ('text', 'voice', 'dm'));

		CREATE TABLE IF NOT EXISTS dm_channels (
			channel_id UUID PRIMARY KEY REFERENCES channels(id) ON DELETE CASCADE,
			user1_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			user2_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
			UNIQUE(user1_id, user2_id)
		);
		CREATE INDEX IF NOT EXISTS idx_dm_user1 ON dm_channels(user1_id);
		CREATE INDEX IF NOT EXISTS idx_dm_user2 ON dm_channels(user2_id);
	`
	if _, err := p.Exec(ctx, incrementalMigrations); err != nil {
		log.Printf("[DB] Note on incremental migration: %v\n", err)
	}

	log.Println("[DB] Schema migrations applied successfully")
	return nil
}

// EnsureAdminUser checks if any admin exists, or seeds the initial admin user (Herzog)
func (p *Pool) EnsureAdminUser(ctx context.Context, username, password string) error {
	var count int
	err := p.QueryRow(ctx, "SELECT COUNT(*) FROM users WHERE role = 'admin'").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash admin password: %w", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO users (username, display_name, password_hash, role)
		VALUES ($1, $2, $3, 'admin')
		ON CONFLICT (username) DO NOTHING
	`, username, username, string(hash))

	if err != nil {
		return fmt.Errorf("failed to insert initial admin user: %w", err)
	}

	log.Printf("[DB] Initial administrator '%s' initialized\n", username)
	return nil
}

// EnsureDefaultChannels checks if any channels exist, and seeds defaults if empty
func (p *Pool) EnsureDefaultChannels(ctx context.Context) error {
	var count int
	err := p.QueryRow(ctx, "SELECT COUNT(*) FROM channels").Scan(&count)
	if err != nil {
		return err
	}

	if count > 0 {
		return nil
	}

	// 1. Text Category & Channels
	var textCatID, voiceCatID string
	err = p.QueryRow(ctx, `
		INSERT INTO categories (name, sort_order)
		VALUES ('Text-Kanäle', 0)
		RETURNING id
	`).Scan(&textCatID)
	if err != nil {
		return fmt.Errorf("failed to create default text category: %w", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO channels (category_id, name, type, topic, sort_order)
		VALUES 
			($1, 'general', 'text', 'Allgemeine Diskussionen & Chat', 0),
			($1, 'medien', 'text', 'Bilder, Screenshots & Clips', 1)
	`, textCatID)
	if err != nil {
		return fmt.Errorf("failed to create default text channels: %w", err)
	}

	// 2. Voice Category & Channels
	err = p.QueryRow(ctx, `
		INSERT INTO categories (name, sort_order)
		VALUES ('Voice-Hangouts', 1)
		RETURNING id
	`).Scan(&voiceCatID)
	if err != nil {
		return fmt.Errorf("failed to create default voice category: %w", err)
	}

	_, err = p.Exec(ctx, `
		INSERT INTO channels (category_id, name, type, topic, sort_order)
		VALUES 
			($1, 'Lounge', 'voice', 'Offener Sprach-Hangout', 0),
			($1, 'Gaming 4K', 'voice', 'Source-Quality Screen & Gaming Hangout', 1)
	`, voiceCatID)
	if err != nil {
		return fmt.Errorf("failed to create default voice channels: %w", err)
	}

	log.Println("[DB] Initial text and voice channels seeded successfully")
	return nil
}
