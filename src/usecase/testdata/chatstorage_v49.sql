-- Schema generated from GOWA v9.7.0 (5e17e8c), migrations 1-49.
BEGIN TRANSACTION;
CREATE TABLE chats (
			jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			name VARCHAR(255) NOT NULL,
			last_message_time TIMESTAMP NOT NULL,
			ephemeral_expiration INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, archived BOOLEAN DEFAULT FALSE,
			PRIMARY KEY (jid, device_id)
		);
CREATE TABLE chatwoot_device_configs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			device_jid VARCHAR(255) NOT NULL DEFAULT '',
			chatwoot_url VARCHAR(512) NOT NULL DEFAULT '',
			account_id INTEGER NOT NULL DEFAULT 0,
			inbox_id INTEGER NOT NULL DEFAULT 0,
			api_token TEXT NOT NULL DEFAULT '',
			enabled BOOLEAN NOT NULL DEFAULT 1,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
CREATE TABLE chatwoot_forward_queue (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			event_name VARCHAR(80) NOT NULL DEFAULT '',
			wa_message_id VARCHAR(255) NOT NULL DEFAULT '',
			payload_json TEXT NOT NULL DEFAULT '',
			attempts INTEGER NOT NULL DEFAULT 0,
			last_error TEXT NOT NULL DEFAULT '',
			next_attempt_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(device_id, event_name, wa_message_id)
		);
CREATE TABLE chatwoot_message_links (
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			wa_message_id VARCHAR(255) NOT NULL,
			wa_chat_jid VARCHAR(255) NOT NULL DEFAULT '',
			chatwoot_message_id INTEGER NOT NULL DEFAULT 0,
			chatwoot_conversation_id INTEGER NOT NULL DEFAULT 0,
			chatwoot_inbox_id INTEGER NOT NULL DEFAULT 0,
			chatwoot_contact_inbox_source_id VARCHAR(255) NOT NULL DEFAULT '',
			source_id VARCHAR(255) NOT NULL DEFAULT '',
			direction VARCHAR(20) NOT NULL DEFAULT '',
			is_read BOOLEAN DEFAULT FALSE,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, chatwoot_config_id INTEGER NOT NULL DEFAULT 0, chatwoot_account_id INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (device_id, wa_message_id)
		);
CREATE TABLE devices (
			device_id VARCHAR(255) PRIMARY KEY,
			display_name VARCHAR(255) DEFAULT '',
			jid VARCHAR(255) DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		, webhook_url TEXT DEFAULT NULL, webhook_secret TEXT DEFAULT '', webhook_events TEXT DEFAULT '', webhook_insecure_skip_verify BOOLEAN DEFAULT FALSE, ad_jid VARCHAR(255) DEFAULT '');
CREATE TABLE message_edits (
			original_message_id VARCHAR(255) NOT NULL,
			edit_event_id VARCHAR(255) NOT NULL,
			chat_jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			editor VARCHAR(255) DEFAULT '',
			previous_content TEXT DEFAULT '',
			new_content TEXT DEFAULT '',
			edited_at TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (original_message_id, edit_event_id, device_id),
			FOREIGN KEY (original_message_id, chat_jid, device_id) REFERENCES messages(id, chat_jid, device_id) ON DELETE CASCADE
		);
CREATE TABLE message_reactions (
			message_id VARCHAR(255) NOT NULL,
			chat_jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			reactor_jid VARCHAR(255) NOT NULL,
			emoji TEXT NOT NULL DEFAULT '',
			is_from_me BOOLEAN DEFAULT FALSE,
			reaction_timestamp TIMESTAMP NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (message_id, reactor_jid, device_id)
		);
CREATE TABLE messages (
			id VARCHAR(255) NOT NULL,
			chat_jid VARCHAR(255) NOT NULL,
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			sender VARCHAR(255) NOT NULL,
			content TEXT,
			timestamp TIMESTAMP NOT NULL,
			is_from_me BOOLEAN DEFAULT FALSE,
			media_type VARCHAR(50),
			filename VARCHAR(255),
			url TEXT,
			media_key BLOB,
			file_sha256 BLOB,
			file_enc_sha256 BLOB,
			file_length INTEGER DEFAULT 0,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP, call_metadata TEXT DEFAULT '', referral_metadata TEXT DEFAULT '', direct_path TEXT DEFAULT '', context_metadata TEXT DEFAULT '',
			PRIMARY KEY (id, chat_jid, device_id)
		);
CREATE TABLE poll_definitions (
			device_id VARCHAR(255) NOT NULL DEFAULT '',
			chat_jid VARCHAR(255) NOT NULL,
			poll_message_id VARCHAR(255) NOT NULL,
			question TEXT NOT NULL DEFAULT '',
			options_json TEXT NOT NULL DEFAULT '[]',
			selectable_option_count INTEGER NOT NULL DEFAULT 0,
			version VARCHAR(16) NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (device_id, chat_jid, poll_message_id)
		);
CREATE TABLE scheduled_sends (
			id VARCHAR(64) PRIMARY KEY,
			device_id VARCHAR(255) NOT NULL,
			message_type VARCHAR(32) NOT NULL,
			payload_json TEXT NOT NULL,
			assets_json TEXT NOT NULL DEFAULT '{}',
			phone VARCHAR(255) NOT NULL DEFAULT '',
			summary TEXT NOT NULL DEFAULT '',
			scheduled_at TIMESTAMP NOT NULL,
			next_run_at TIMESTAMP NOT NULL,
			timezone VARCHAR(128) NOT NULL,
			recurrence VARCHAR(16) NOT NULL DEFAULT 'once',
			weekdays_json TEXT NOT NULL DEFAULT '[]',
			day_of_month INTEGER NOT NULL DEFAULT 0,
			end_at TIMESTAMP NULL,
			occurrence_limit INTEGER NOT NULL DEFAULT 0,
			occurrence_count INTEGER NOT NULL DEFAULT 0,
			attempts INTEGER NOT NULL DEFAULT 0,
			status VARCHAR(16) NOT NULL DEFAULT 'active',
			lease_token VARCHAR(64) NOT NULL DEFAULT '',
			lease_until TIMESTAMP NULL,
			last_run_at TIMESTAMP NULL,
			last_message_id VARCHAR(255) NOT NULL DEFAULT '',
			last_error TEXT NOT NULL DEFAULT '',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
CREATE TABLE schema_info (version INTEGER PRIMARY KEY, updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP);
CREATE INDEX idx_messages_chat_jid ON messages(chat_jid);
CREATE INDEX idx_messages_device ON messages(device_id);
CREATE INDEX idx_messages_timestamp ON messages(timestamp);
CREATE INDEX idx_messages_media_type ON messages(media_type);
CREATE INDEX idx_messages_sender ON messages(sender);
CREATE INDEX idx_chats_last_message ON chats(last_message_time);
CREATE INDEX idx_chats_name ON chats(name);
CREATE INDEX idx_chats_device ON chats(device_id);
CREATE INDEX idx_devices_created_at ON devices(created_at);
CREATE INDEX idx_chats_archived ON chats(archived);
CREATE INDEX idx_message_reactions_lookup ON message_reactions(device_id, chat_jid, message_id);
CREATE INDEX idx_message_edits_original ON message_edits(original_message_id);
CREATE INDEX idx_message_edits_device ON message_edits(device_id);
CREATE INDEX idx_message_edits_edited_at ON message_edits(edited_at);
CREATE INDEX idx_chatwoot_links_chatwoot_id ON chatwoot_message_links(device_id, chatwoot_message_id);
CREATE INDEX idx_chatwoot_links_source_id ON chatwoot_message_links(device_id, source_id);
CREATE INDEX idx_chatwoot_links_conversation ON chatwoot_message_links(chatwoot_conversation_id, updated_at);
CREATE INDEX idx_chatwoot_links_unread ON chatwoot_message_links(device_id, wa_chat_jid, direction, is_read, updated_at, created_at);
CREATE INDEX idx_chatwoot_forward_queue_due ON chatwoot_forward_queue(next_attempt_at, id);
CREATE UNIQUE INDEX idx_chatwoot_device_configs_device ON chatwoot_device_configs(device_id);
CREATE UNIQUE INDEX idx_chatwoot_device_configs_jid ON chatwoot_device_configs(device_jid) WHERE device_jid <> '';
CREATE UNIQUE INDEX idx_chatwoot_device_configs_inbox ON chatwoot_device_configs(chatwoot_url, account_id, inbox_id);
CREATE INDEX idx_chatwoot_links_conversation_account ON chatwoot_message_links(chatwoot_conversation_id, chatwoot_account_id, updated_at);
CREATE INDEX idx_chatwoot_links_config ON chatwoot_message_links(chatwoot_config_id);
CREATE INDEX idx_messages_chat_device_timestamp ON messages(chat_jid, device_id, timestamp);
CREATE INDEX idx_scheduled_sends_due ON scheduled_sends(status, next_run_at);
CREATE INDEX idx_scheduled_sends_device ON scheduled_sends(device_id, status, next_run_at);
DELETE FROM "sqlite_sequence";
INSERT INTO schema_info (version, updated_at) VALUES (49, '2026-10-24 00:00:00');
COMMIT;
