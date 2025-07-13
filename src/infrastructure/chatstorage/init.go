package chatstorage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "github.com/mattn/go-sqlite3"
)

// Config holds configuration for chat storage
type Config struct {
	DatabasePath      string
	EnableForeignKeys bool
	EnableWAL         bool
}

// DefaultConfig returns default configuration
func DefaultConfig() *Config {
	return &Config{
		DatabasePath:      "storages/chatstorage.db",
		EnableForeignKeys: true,
		EnableWAL:         true,
	}
}

// Storage manages the chat storage system
type Storage struct {
	db     *sql.DB
	repo   Repository
	config *Config
}

// NewStorage creates a new Storage instance
func NewStorage(config *Config) (*Storage, error) {
	if config == nil {
		config = DefaultConfig()
	}

	// Create directory for database if it doesn't exist
	dir := filepath.Dir(config.DatabasePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create database directory: %w", err)
	}

	// Build connection string
	connStr := fmt.Sprintf("file:%s?_journal_mode=WAL", config.DatabasePath)
	if config.EnableForeignKeys {
		connStr += "&_foreign_keys=on"
	}

	// Open database connection
	db, err := sql.Open("sqlite3", connStr)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Test connection
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	storage := &Storage{
		db:     db,
		config: config,
		repo:   NewSQLiteRepository(db),
	}

	// Initialize database schema
	if err := storage.initializeSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	return storage, nil
}

// Repository returns the repository instance
func (s *Storage) Repository() Repository {
	return s.repo
}

// DB returns the raw database connection
func (s *Storage) DB() *sql.DB {
	return s.db
}

// Close closes the database connection
func (s *Storage) Close() error {
	if s.db == nil {
		return nil
	}
	
	// Close the database connection
	if err := s.db.Close(); err != nil {
		return fmt.Errorf("failed to close database connection: %w", err)
	}
	
	return nil
}

// initializeSchema creates or migrates the database schema
func (s *Storage) initializeSchema() error {
	// Get current schema version
	version, err := s.getSchemaVersion()
	if err != nil {
		return err
	}

	// Run migrations based on version
	migrations := s.getMigrations()
	for i := version; i < len(migrations); i++ {
		if err := s.runMigration(migrations[i], i+1); err != nil {
			return fmt.Errorf("failed to run migration %d: %w", i+1, err)
		}
	}

	return nil
}

// getSchemaVersion returns the current schema version
func (s *Storage) getSchemaVersion() (int, error) {
	// Create schema_info table if it doesn't exist
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_info (
			version INTEGER PRIMARY KEY DEFAULT 0,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		return 0, err
	}

	// Get current version
	var version int
	err = s.db.QueryRow("SELECT COALESCE(MAX(version), 0) FROM schema_info").Scan(&version)
	if err != nil {
		return 0, err
	}

	return version, nil
}

// runMigration executes a migration
func (s *Storage) runMigration(migration string, version int) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Execute migration
	if _, err := tx.Exec(migration); err != nil {
		return err
	}

	// Update schema version
	if _, err := tx.Exec("INSERT OR REPLACE INTO schema_info (version) VALUES (?)", version); err != nil {
		return err
	}

	return tx.Commit()
}

// getMigrations returns all database migrations
func (s *Storage) getMigrations() []string {
	return []string{
		// Migration 1: Initial schema with only chats and messages tables
		`
		-- Create chats table
		CREATE TABLE IF NOT EXISTS chats (
			jid TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			last_message_time TIMESTAMP NOT NULL,
			ephemeral_expiration INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);

		-- Create messages table
		CREATE TABLE IF NOT EXISTS messages (
			id TEXT NOT NULL,
			chat_jid TEXT NOT NULL,
			sender TEXT NOT NULL,
			content TEXT,
			timestamp TIMESTAMP NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			media_type TEXT,
			filename TEXT,
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (id, chat_jid),
			FOREIGN KEY (chat_jid) REFERENCES chats(jid) ON DELETE CASCADE
		);

		-- Create indexes for performance
		CREATE INDEX IF NOT EXISTS idx_messages_chat_jid ON messages(chat_jid);
		CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp);
		CREATE INDEX IF NOT EXISTS idx_messages_media_type ON messages(media_type);
		CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages(sender);
		CREATE INDEX IF NOT EXISTS idx_chats_last_message ON chats(last_message_time);
		CREATE INDEX IF NOT EXISTS idx_chats_name ON chats(name);
		`,
		
		// Migration 2: Add index for message ID lookups (performance optimization)
		`
		CREATE INDEX IF NOT EXISTS idx_messages_id ON messages(id);
		`,
	}
}
