package pgimport

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
)

// txFatalError signals that the database transaction is in an unrecoverable
// state (e.g. connection lost, SAVEPOINT creation failed). The message loop
// in ImportChat checks for this to abort early instead of churning through
// remaining messages that will all fail.
type txFatalError struct{ cause error }

func (e txFatalError) Error() string { return e.cause.Error() }
func (e txFatalError) Unwrap() error { return e.cause }

// ImportChatRequest carries everything the importer needs to write one
// WhatsApp chat's worth of history into Chatwoot. The caller is
// responsible for:
//
//   - resolving `ChatName` to a real display name (group subject, push
//     name, or fallback phone) BEFORE calling; the importer will not
//     look names up. This is deliberate — the SyncService already has a
//     whatsmeow client handy, and routing GetGroupInfo through the
//     importer would duplicate logic from webhook_forward.go.
//   - sorting `Messages` in chronological order (oldest first), matching
//     how the REST path orders them.
type ImportChatRequest struct {
	ChatJID  string
	ChatName string
	Messages []*domainChatStorage.Message
}

// ImportResult reports per-chat counts back to the SyncService so it can
// update its progress tracker in the same shape as the REST path.
type ImportResult struct {
	ContactID       int
	ConversationID  int
	MessagesWrote   int
	MessagesSkipped int // already present (idempotent replay)
	MessagesFailed  int
	Links           []domainChatStorage.ChatwootMessageLink
}

// ImportChat is the single public entry point for writing historical
// messages into Chatwoot. Everything happens inside one transaction per
// chat so that a mid-import failure rolls back cleanly and the chat can
// be retried without producing duplicates.
//
// Per-message errors (unexpected NULLs, schema drift, etc.) do NOT abort
// the whole transaction — each message row is wrapped in a SAVEPOINT so
// one bad message only loses itself. This is critical when importing
// tens of thousands of rows.
func (i *Importer) ImportChat(ctx context.Context, req ImportChatRequest) (*ImportResult, error) {
	if i.closed.Load() {
		return nil, fmt.Errorf("pgimport: importer is closed")
	}
	if req.ChatJID == "" {
		return nil, fmt.Errorf("pgimport: empty ChatJID")
	}

	res := &ImportResult{}

	tx, err := i.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return nil, fmt.Errorf("pgimport: begin tx: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()

	isGroup := isGroupJID(req.ChatJID)

	contactID, err := i.upsertContact(ctx, tx, req.ChatJID, req.ChatName, isGroup)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("pgimport: upsert contact: %w", err)
	}
	res.ContactID = contactID

	contactInboxID, err := i.upsertContactInbox(ctx, tx, contactID, req.ChatJID)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("pgimport: upsert contact_inbox: %w", err)
	}

	convID, err := i.findOrCreateConversation(ctx, tx, contactID, contactInboxID, req.Messages)
	if err != nil {
		_ = tx.Rollback()
		return nil, fmt.Errorf("pgimport: conversation: %w", err)
	}
	res.ConversationID = convID

	var lastActivity time.Time
	for _, msg := range req.Messages {
		if err := ctx.Err(); err != nil {
			_ = tx.Rollback()
			return res, err
		}

		link, wrote, err := i.insertMessageSavepoint(ctx, tx, convID, contactID, msg, isGroup)
		switch {
		case errors.As(err, &txFatalError{}):
			// Transaction is broken — abort the loop. The caller (syncChatPG)
			// counts every message as failed when ImportChat returns an error,
			// so we deliberately do NOT increment res.MessagesFailed here to
			// avoid double-counting.
			_ = tx.Rollback()
			return res, fmt.Errorf("pgimport: transaction-fatal error on message %s: %w", msg.ID, err)
		case err != nil:
			res.MessagesFailed++
			logrus.Warnf("Chatwoot pgimport: message %s failed: %v", msg.ID, err)
		case !wrote:
			res.MessagesSkipped++
		default:
			res.MessagesWrote++
			if msg.Timestamp.After(lastActivity) {
				lastActivity = msg.Timestamp
			}
		}
		if link != nil {
			res.Links = append(res.Links, *link)
		}
	}

	if res.MessagesWrote > 0 {
		// Fall back to now() if every written message carried a zero
		// timestamp (shouldn't happen in practice, but prevents the
		// conversation landing at year 0001 in Chatwoot's agent UI).
		if lastActivity.IsZero() {
			lastActivity = time.Now()
		}
		if err := i.touchConversation(ctx, tx, convID, lastActivity); err != nil {
			_ = tx.Rollback()
			return res, fmt.Errorf("pgimport: touch conversation %d: %w", convID, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return res, fmt.Errorf("pgimport: commit: %w", err)
	}
	return res, nil
}

// upsertContact finds a Chatwoot contact whose custom_attributes.gowa_whatsapp_jid
// matches the chat JID, or creates one if absent. The matching key is the
// same one the REST client writes at client.go:231, so a contact created by
// the live path and a contact looked up by the importer resolve to the
// same row.
func (i *Importer) upsertContact(ctx context.Context, tx *sql.Tx, jid, name string, isGroup bool) (int, error) {
	// Fast path: look up by our custom attribute.
	var id int
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM contacts
		WHERE account_id = $1
		  AND (custom_attributes->>'gowa_whatsapp_jid') = $2
		LIMIT 1
	`, i.accountID, jid).Scan(&id)
	if err == nil {
		// Preserve manually edited 1:1 contact names; group subjects can
		// legitimately change and should keep following WhatsApp.
		if isGroup && name != "" {
			if _, err := tx.ExecContext(ctx, `
				UPDATE contacts
				SET name = $1, updated_at = now()
				WHERE id = $2 AND account_id = $3 AND (name IS DISTINCT FROM $1)
			`, name, id, i.accountID); err != nil {
				return 0, fmt.Errorf("refresh contact name: %w", err)
			}
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Fallback lookup by phone number for 1:1 chats — matches the REST
	// client's FindContactByIdentifier path and avoids creating duplicate
	// contacts when the live REST sync already landed one without the
	// gowa_whatsapp_jid attribute.
	phone, identifier, displayName := contactIdentity(jid, name)
	if phone != "" {
		var existing int
		err := tx.QueryRowContext(ctx, `
			SELECT id
			FROM contacts
			WHERE account_id = $1 AND phone_number = $2
			LIMIT 1
		`, i.accountID, phone).Scan(&existing)
		if err == nil {
			// Attach our JID attribute so future lookups find it on the fast path.
			if _, err := tx.ExecContext(ctx, `
				UPDATE contacts
				SET custom_attributes = COALESCE(custom_attributes, '{}'::jsonb)
				                        || jsonb_build_object('gowa_whatsapp_jid', $1::text),
				    updated_at = now()
				WHERE id = $2
			`, jid, existing); err != nil {
				return 0, fmt.Errorf("attach contact JID attribute: %w", err)
			}
			return existing, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}

	// Brand new contact.
	customAttrs, _ := json.Marshal(map[string]any{
		"gowa_whatsapp_jid": jid,
	})

	var newID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO contacts
			(account_id, name, phone_number, identifier,
			 custom_attributes, additional_attributes, created_at, updated_at)
		VALUES
			($1, $2, NULLIF($3, ''), NULLIF($4, ''),
			 $5::jsonb, '{}'::jsonb, now(), now())
		RETURNING id
	`, i.accountID, displayName, phone, identifier, string(customAttrs)).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("insert contact: %w", err)
	}
	logrus.Debugf("Chatwoot pgimport: created contact id=%d jid=%s isGroup=%v", newID, jid, isGroup)
	return newID, nil
}

// upsertContactInbox links a contact to the configured inbox. Chatwoot
// requires this row before a conversation can be opened — the inbox-side
// `source_id` uniquely identifies the customer within an API channel.
// Uses ON CONFLICT on the (inbox_id, source_id) UNIQUE index so a race
// with the live REST path (which may create the same row concurrently)
// resolves to a single winning row instead of a 23505 violation.
func (i *Importer) upsertContactInbox(ctx context.Context, tx *sql.Tx, contactID int, jid string) (int, error) {
	var id int
	err := tx.QueryRowContext(ctx, `
		SELECT id
		FROM contact_inboxes
		WHERE contact_id = $1 AND inbox_id = $2
		LIMIT 1
	`, contactID, i.inboxID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Chatwoot's Rails model generates pubsub_token via a callback. In raw
	// SQL we produce our own — gen_random_uuid() is guaranteed present:
	// Chatwoot's schema.rb calls `enable_extension "pgcrypto"` at the top.
	var newID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO contact_inboxes
			(contact_id, inbox_id, source_id, hmac_verified,
			 pubsub_token, created_at, updated_at)
		VALUES
			($1, $2, $3, false,
			 replace(gen_random_uuid()::text, '-', ''), now(), now())
		ON CONFLICT (inbox_id, source_id) DO UPDATE
			SET updated_at = EXCLUDED.updated_at
		RETURNING id
	`, contactID, i.inboxID, jid).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("insert contact_inbox: %w", err)
	}
	return newID, nil
}

// conversationStatusForNew returns the Chatwoot conversation status enum a
// newly created (or reopened) conversation should land in. It mirrors the REST
// client's conversationStatusForNew() (client.go) so both paths honor the
// CHATWOOT_CONVERSATION_PENDING setting consistently: `pending` (the unassigned
// queue) when enabled, otherwise `open` (the agent's active queue).
func conversationStatusForNew() int {
	if config.ChatwootConversationPending {
		return conversationStatusPending
	}
	return conversationStatusOpen
}

// findOrCreateConversation returns the id of the most recent conversation
// for (account, inbox, contact), opening a fresh one if none exists. We
// match regardless of `status` so that a conversation resolved by an agent
// and then re-imported from WhatsApp reuses the same row instead of
// creating a duplicate (the live REST path hits the same row for new
// inbound messages, so the two paths agree). When CHATWOOT_REOPEN_CONVERSATION
// is enabled (the default), a reused *resolved* conversation is flipped back to
// the configured new-status so a returning customer's history resurfaces in the
// agent queue — matching the REST path's reopen behavior. (When reopen is
// disabled the REST path opens a brand-new conversation instead; the importer
// still reuses the row here to avoid duplicate threads, leaving its status
// untouched. This is a deliberate, narrow asymmetry: the live REST path opens
// a fresh thread when reopen is off, while this importer reuses any existing
// conversation regardless of status and never spawns a second one — keeping a
// backfilled history in a single thread.)
//
// We deliberately do NOT supply `display_id`. Chatwoot installs a
// BEFORE INSERT trigger `conversations_before_insert_row_tr` that
// unconditionally assigns `NEW.display_id := nextval('conv_dpid_seq_' || account_id)`,
// so any value we pass would be thrown away. Letting the trigger fill it
// avoids racing the per-account sequence with the live Rails path.
//
// `gen_random_uuid()` is guaranteed present: Chatwoot's schema.rb calls
// `enable_extension "pgcrypto"` at the top.
func (i *Importer) findOrCreateConversation(
	ctx context.Context,
	tx *sql.Tx,
	contactID, contactInboxID int,
	msgs []*domainChatStorage.Message,
) (int, error) {
	var id, status int
	err := tx.QueryRowContext(ctx, `
		SELECT id, status
		FROM conversations
		WHERE account_id = $1
		  AND inbox_id = $2
		  AND contact_id = $3
		ORDER BY id DESC
		LIMIT 1
	`, i.accountID, i.inboxID, contactID).Scan(&id, &status)
	if err == nil {
		// Reopen a reused resolved thread when configured. Only `resolved`
		// is touched (pending/snoozed reused rows are left as the agent set
		// them), matching the REST path. The UPDATE is idempotent via its
		// WHERE status guard.
		if config.ChatwootReopenConversation && status == conversationStatusResolved {
			if _, uerr := tx.ExecContext(ctx, `
				UPDATE conversations
				SET status = $1, updated_at = now()
				WHERE id = $2 AND account_id = $3 AND status = $4
			`, conversationStatusForNew(), id, i.accountID, conversationStatusResolved); uerr != nil {
				return 0, fmt.Errorf("reopen conversation %d: %w", id, uerr)
			}
		}
		return id, nil
	}
	if err != sql.ErrNoRows {
		return 0, err
	}

	// Anchor the new conversation's created_at at the first message's
	// timestamp when available, so Chatwoot's UI sorts it correctly. Fall
	// back to now() for empty or zero-time message slices.
	var createdAt time.Time
	if len(msgs) > 0 && !msgs[0].Timestamp.IsZero() {
		createdAt = msgs[0].Timestamp
	} else {
		createdAt = time.Now()
	}

	// `waiting_since = created_at` matches Rails' before_create callback
	// (`ensure_waiting_since`). Without it, every imported conversation
	// is perpetually flagged as "unattended" in Chatwoot's dashboard scope.
	var newID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO conversations
			(account_id, inbox_id, status, contact_id, contact_inbox_id,
			 uuid, additional_attributes, custom_attributes,
			 last_activity_at, waiting_since, created_at, updated_at)
		VALUES
			($1, $2, $3, $4, $5,
			 gen_random_uuid(),
			 '{}'::jsonb, '{}'::jsonb,
			 $6, $6, $6, now())
		RETURNING id
	`, i.accountID, i.inboxID, conversationStatusForNew(), contactID, contactInboxID, createdAt).Scan(&newID)
	if err != nil {
		return 0, fmt.Errorf("insert conversation: %w", err)
	}
	logrus.Debugf("Chatwoot pgimport: created conversation id=%d contact=%d", newID, contactID)
	return newID, nil
}

// insertMessageSavepoint wraps one INSERT in a SAVEPOINT so a single bad
// row doesn't abort the surrounding chat transaction. Returns (wrote,
// err) — wrote=false, err=nil means the row was already present and was
// skipped idempotently.
func (i *Importer) insertMessageSavepoint(
	ctx context.Context,
	tx *sql.Tx,
	convID, contactID int,
	msg *domainChatStorage.Message,
	isGroup bool,
) (*domainChatStorage.ChatwootMessageLink, bool, error) {
	const savepoint = "cw_msg"

	if _, err := tx.ExecContext(ctx, "SAVEPOINT "+savepoint); err != nil {
		// SAVEPOINT failure means the transaction itself is broken (connection
		// lost, aborted state). Wrap as txFatalError so the caller aborts the loop.
		return nil, false, txFatalError{err}
	}

	link, wrote, err := i.insertMessage(ctx, tx, convID, contactID, msg, isGroup)
	if err != nil {
		if _, rbErr := tx.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+savepoint); rbErr != nil {
			return nil, false, txFatalError{fmt.Errorf("%v (rollback failed: %v)", err, rbErr)}
		}
		return nil, false, err
	}
	if _, err := tx.ExecContext(ctx, "RELEASE SAVEPOINT "+savepoint); err != nil {
		// RELEASE almost never fails on a healthy connection; when it does
		// the connection is wedged and every subsequent SAVEPOINT will fail.
		// Propagate as txFatalError so the caller aborts the loop cleanly.
		return link, wrote, txFatalError{err}
	}
	return link, wrote, nil
}

// insertMessage does the actual write. It skips rows that already exist
// (matched by source_id within the inbox) so re-runs are safe even when
// the previous conversation was resolved and re-opened as a new row.
func (i *Importer) insertMessage(
	ctx context.Context,
	tx *sql.Tx,
	convID, contactID int,
	msg *domainChatStorage.Message,
	isGroup bool,
) (*domainChatStorage.ChatwootMessageLink, bool, error) {
	if msg.ID == "" {
		return nil, false, fmt.Errorf("empty message ID")
	}

	// WAID: prefix matches the source_id convention Chatwoot's WhatsApp channel
	// integrations use for WhatsApp message IDs. Using the same format means a
	// Chatwoot instance that already imported these messages through another
	// integration recognizes already-imported rows on re-sync instead of
	// double-inserting.
	sourceID := "WAID:" + msg.ID

	// Idempotency probe keyed to the inbox: Chatwoot's `index_messages_on_source_id`
	// is plain (non-unique), but messages imported under the same inbox
	// share a source_id iff they're the same WhatsApp message — whether
	// they landed in a different conversation after a resolve/reopen cycle
	// or not. Inbox-scoped lookup makes replays correctly skip duplicates.
	var existingID, existingConvID int
	err := tx.QueryRowContext(ctx, `
		SELECT id, conversation_id FROM messages
		WHERE inbox_id = $1 AND source_id = $2
		LIMIT 1
	`, i.inboxID, sourceID).Scan(&existingID, &existingConvID)
	if err == nil {
		return i.buildMessageLink(msg, existingConvID, existingID, sourceID, messageTypeForWA(msg.IsFromMe)), false, nil
	}
	if err != sql.ErrNoRows {
		return nil, false, err
	}

	content := buildContent(msg, isGroup)
	// Skip entirely when there is nothing meaningful to show — no body, no
	// media placeholder. Writing blank rows pollutes Chatwoot's UI.
	if content == "" {
		return nil, false, nil
	}
	mType := messageTypeForWA(msg.IsFromMe)
	// Historical messages were all successfully delivered by definition (we're
	// pulling them from our local chat store), so "delivered" is the honest default.
	status := messageStatusDelivered

	// additional_attributes carries metadata useful for debugging and for
	// agents who want to see the original WhatsApp identifiers. We populate
	// this column because the cost is zero and the extra traceability helps
	// when correlating Chatwoot rows back to chat-storage entries.
	addl, _ := json.Marshal(map[string]any{
		"wa_message_id": msg.ID,
		"wa_chat_jid":   msg.ChatJID,
		"wa_sender":     msg.Sender,
		"wa_media_type": msg.MediaType,
	})

	// senderType/senderID: for incoming messages the sender is the Contact
	// row we already upserted. For outgoing messages Chatwoot expects a
	// User (or AgentBot) — the row is rendered as "Unknown sender" if these
	// stay NULL. We resolve the API-token's owner from `access_tokens` once
	// at startup
	// (see Importer.resolveAgent) and stamp every outgoing imported row
	// with that user. When the lookup failed (no token, missing row,
	// table unreachable) i.agentUserID is 0 and we deliberately leave the
	// columns NULL — the message still imports, just without attribution.
	var senderType sql.NullString
	var senderID sql.NullInt64
	if msg.IsFromMe {
		if i.agentUserID != 0 && i.agentUserType != "" {
			senderType = sql.NullString{String: i.agentUserType, Valid: true}
			senderID = sql.NullInt64{Int64: i.agentUserID, Valid: true}
		}
	} else {
		senderType = sql.NullString{String: senderTypeContact, Valid: true}
		senderID = sql.NullInt64{Int64: int64(contactID), Valid: true}
	}

	// processed_message_content mirrors `content` so Chatwoot's full-text
	// search over conversations returns hits for imported messages. Rails
	// fills this via a before_save callback; direct SQL must do it manually.
	var chatwootMessageID int
	err = tx.QueryRowContext(ctx, `
		INSERT INTO messages
			(content, processed_message_content,
			 account_id, inbox_id, conversation_id,
			 message_type, created_at, updated_at, private, status,
			 source_id, content_type, content_attributes,
			 sender_type, sender_id, additional_attributes)
		VALUES
			($1, $1,
			 $2, $3, $4,
			 $5, $6, $6, false, $7,
			 $8, $9, '{}'::jsonb,
			 $10, $11, $12::jsonb)
		RETURNING id
	`,
		content,
		i.accountID, i.inboxID, convID,
		mType, msg.Timestamp, status,
		sourceID, contentTypeText,
		senderType, senderID, string(addl),
	).Scan(&chatwootMessageID)
	if err != nil {
		return nil, false, err
	}
	return i.buildMessageLink(msg, convID, chatwootMessageID, sourceID, mType), true, nil
}

func (i *Importer) buildMessageLink(msg *domainChatStorage.Message, convID, chatwootMessageID int, sourceID string, messageType int) *domainChatStorage.ChatwootMessageLink {
	if msg == nil || msg.ID == "" || msg.DeviceID == "" || chatwootMessageID == 0 {
		return nil
	}

	direction := "incoming"
	if messageType == messageTypeOutgoing {
		direction = "outgoing"
	}

	return &domainChatStorage.ChatwootMessageLink{
		DeviceID:                     msg.DeviceID,
		WhatsAppMessageID:            msg.ID,
		WhatsAppChatJID:              msg.ChatJID,
		ChatwootMessageID:            chatwootMessageID,
		ChatwootConversationID:       convID,
		ChatwootInboxID:              i.inboxID,
		ChatwootContactInboxSourceID: msg.ChatJID,
		SourceID:                     sourceID,
		Direction:                    direction,
		IsRead:                       false,
		// Account scope must be stamped here: pgimport is legacy-only (config id
		// stays 0) but a zero account id would only match through the legacy-zero
		// wildcard, which per-device mode disables — and the boot-time backfill
		// only repairs rows existing at startup.
		ChatwootAccountID: i.accountID,
	}
}

// touchConversation advances last_activity_at on the conversation row so
// that Chatwoot's agent UI orders it correctly in the conversation list.
// Not strictly required, but matches how the Rails callbacks behave.
func (i *Importer) touchConversation(ctx context.Context, tx *sql.Tx, convID int, at time.Time) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE conversations
		SET last_activity_at = GREATEST(COALESCE(last_activity_at, $2), $2),
		    updated_at = now()
		WHERE id = $1
	`, convID, at)
	return err
}

// buildContent computes the message body text for the Chatwoot messages
// table. Unlike the REST path in sync.go, this deliberately does NOT
// prepend "[YYYY-MM-DD HH:MM]" — the whole point of the direct-DB
// importer is that we can preserve real timestamps on the row itself, so
// polluting the body with a string copy is unnecessary.
//
// For group messages, we still prefix the sender so agents can tell
// participants apart in Chatwoot's single conversation thread. That is a
// semantic prefix, not a timestamp workaround, and it's consistent with
// how the Chatwoot agent UI renders group chats everywhere else.
func buildContent(msg *domainChatStorage.Message, isGroup bool) string {
	body := strings.TrimSpace(msg.Content)

	if body == "" && msg.MediaType != "" {
		if config.ChatwootImportPlaceholderMediaMessage {
			body = fmt.Sprintf("[%s]", msg.MediaType)
		}
	}

	if isGroup && !msg.IsFromMe && msg.Sender != "" {
		// The sender label is the JID user portion — a phone number for normal
		// users, an @lid identifier for privacy-masked senders. This matches the
		// REST path (sync.go:syncMessage), which labels group senders the same way.
		senderName := utils.ExtractPhoneFromJID(msg.Sender)
		if body == "" {
			return senderName + ":"
		}
		return senderName + ": " + body
	}
	return body
}
