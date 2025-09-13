package webhook

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	"github.com/google/uuid"
)

type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(db *sql.DB) webhook.IWebhookRepository {
	return &SQLiteRepository{db: db}
}

func (r *SQLiteRepository) InitializeSchema() error {
	// Create the table with all columns
	query := `
		CREATE TABLE IF NOT EXISTS webhooks (
			id TEXT PRIMARY KEY,
			url TEXT NOT NULL,
			secret TEXT,
			events TEXT NOT NULL DEFAULT '[]',
			enabled BOOLEAN DEFAULT TRUE,
			description TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`
	_, err := r.db.Exec(query)
	if err != nil {
		return err
	}
	
	// Add any missing columns
	columnsToCheck := []string{"secret", "events", "enabled", "description", "created_at", "updated_at"}
	
	for _, column := range columnsToCheck {
		var count int
		err = r.db.QueryRow(`SELECT COUNT(*) FROM pragma_table_info('webhooks') WHERE name = ?`, column).Scan(&count)
		if err != nil {
			return err
		}
		
		if count == 0 {
			switch column {
			case "secret":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN secret TEXT`)
			case "events":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN events TEXT NOT NULL DEFAULT '[]'`)
			case "enabled":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN enabled BOOLEAN DEFAULT TRUE`)
			case "description":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN description TEXT`)
			case "created_at":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
			case "updated_at":
				_, err = r.db.Exec(`ALTER TABLE webhooks ADD COLUMN updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP`)
			}
			
			if err != nil {
				return err
			}
		}
	}
	
	// Create indexes
	indexQueries := []string{
		"CREATE INDEX IF NOT EXISTS idx_webhooks_enabled ON webhooks(enabled)",
		"CREATE INDEX IF NOT EXISTS idx_webhooks_created_at ON webhooks(created_at)",
	}
	
	for _, indexQuery := range indexQueries {
		_, err = r.db.Exec(indexQuery)
		if err != nil {
			return err
		}
	}
	
	return nil
}

func (r *SQLiteRepository) Create(wh *webhook.Webhook) error {
	if wh.ID == "" {
		wh.ID = uuid.New().String()
	}
	
	eventsJSON, err := json.Marshal(wh.Events)
	if err != nil {
		return err
	}
	
	now := time.Now()
	wh.CreatedAt = now
	wh.UpdatedAt = now
	
	query := `INSERT INTO webhooks (id, url, secret, events, enabled, description, created_at, updated_at) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`
	
	_, err = r.db.Exec(query, wh.ID, wh.URL, wh.Secret, string(eventsJSON), wh.Enabled, wh.Description, wh.CreatedAt, wh.UpdatedAt)
	return err
}

func (r *SQLiteRepository) FindAll() ([]*webhook.Webhook, error) {
	rows, err := r.db.Query(`
		SELECT id, url, secret, events, enabled, description, created_at, updated_at 
		FROM webhooks ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var webhooks []*webhook.Webhook
	for rows.Next() {
		wh, err := r.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	
	return webhooks, rows.Err()
}

func (r *SQLiteRepository) FindByID(id string) (*webhook.Webhook, error) {
	row := r.db.QueryRow(`
		SELECT id, url, secret, events, enabled, description, created_at, updated_at 
		FROM webhooks WHERE id = ?
	`, id)
	
	return r.scanWebhook(row)
}

func (r *SQLiteRepository) Update(wh *webhook.Webhook) error {
	eventsJSON, err := json.Marshal(wh.Events)
	if err != nil {
		return err
	}
	
	wh.UpdatedAt = time.Now()
	
	query := `UPDATE webhooks SET url = ?, secret = ?, events = ?, enabled = ?, description = ?, updated_at = ? 
		WHERE id = ?`
	
	_, err = r.db.Exec(query, wh.URL, wh.Secret, string(eventsJSON), wh.Enabled, wh.Description, wh.UpdatedAt, wh.ID)
	return err
}

func (r *SQLiteRepository) Delete(id string) error {
	_, err := r.db.Exec("DELETE FROM webhooks WHERE id = ?", id)
	return err
}

func (r *SQLiteRepository) FindByEvent(event string) ([]*webhook.Webhook, error) {
	rows, err := r.db.Query(`
		SELECT id, url, secret, events, enabled, description, created_at, updated_at 
		FROM webhooks WHERE enabled = TRUE AND events LIKE ?
	`, "%"+event+"%")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var webhooks []*webhook.Webhook
	for rows.Next() {
		wh, err := r.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		
		for _, e := range wh.Events {
			if e == event {
				webhooks = append(webhooks, wh)
				break
			}
		}
	}
	
	return webhooks, rows.Err()
}

func (r *SQLiteRepository) FindEnabled() ([]*webhook.Webhook, error) {
	rows, err := r.db.Query(`
		SELECT id, url, secret, events, enabled, description, created_at, updated_at 
		FROM webhooks WHERE enabled = TRUE ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	
	var webhooks []*webhook.Webhook
	for rows.Next() {
		wh, err := r.scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		webhooks = append(webhooks, wh)
	}
	
	return webhooks, rows.Err()
}

func (r *SQLiteRepository) scanWebhook(scanner interface{ Scan(...any) error }) (*webhook.Webhook, error) {
	var wh webhook.Webhook
	var eventsJSON string
	var secret, description sql.NullString
	
	err := scanner.Scan(
		&wh.ID, &wh.URL, &secret, &eventsJSON, &wh.Enabled, &description, &wh.CreatedAt, &wh.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	
	if secret.Valid {
		wh.Secret = secret.String
	}
	if description.Valid {
		wh.Description = description.String
	}
	
	if err := json.Unmarshal([]byte(eventsJSON), &wh.Events); err != nil {
		return nil, err
	}
	
	return &wh, nil
}