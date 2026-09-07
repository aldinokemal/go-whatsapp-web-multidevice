package chatwoot

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/chatwoot/pgimport"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
)

// SyncService handles message history synchronization to Chatwoot
type SyncService struct {
	client          *Client
	chatStorageRepo domainChatStorage.IChatStorageRepository

	// Track sync progress per device
	progressMap map[string]*SyncProgress
	progressMu  sync.RWMutex

	// pgImporter is the optional direct-Postgres history importer. When
	// non-nil (i.e. when config.ChatwootImportDBURI is set), SyncHistory
	// writes rows into Chatwoot's schema directly instead of replaying
	// every message through the REST API. Live message forwarding and
	// inbound handling always use the REST client, regardless of this.
	pgImporter *pgimport.Importer
	pgInitMu   sync.Mutex

	// allowPgImport gates the direct-Postgres import path. It is true only for
	// the legacy/env config: ChatwootImportDBURI is a single global DSN, so
	// per-device configs (which may target different Chatwoot databases) use the
	// REST import path instead.
	allowPgImport bool

	// configID is the chatwoot_device_configs row id this service syncs for (0 =
	// legacy/env). Stamped onto message links so reverse routing is config-scoped.
	configID int64

	// mediaDownloader replaces downloadMedia when set. Only tests set it: the
	// real download needs a live whatsmeow client, and the REST media pre-pass
	// cannot be exercised past the download step without one.
	mediaDownloader func(ctx context.Context, msg *domainChatStorage.Message, waClient *whatsmeow.Client) (string, error)
}

// NewSyncService creates a new sync service instance
func NewSyncService(
	client *Client,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
) *SyncService {
	return &SyncService{
		client:          client,
		chatStorageRepo: chatStorageRepo,
		progressMap:     make(map[string]*SyncProgress),
	}
}

// groupNameResolver memoizes GetGroupInfo lookups for the duration of a
// single sync run. Scoping the cache per-run (rather than per-SyncService)
// avoids cross-device pollution when two devices sync concurrently and may
// see different subjects for the same group.
type groupNameResolver struct {
	mu    sync.Mutex
	cache map[string]string
}

func newGroupNameResolver() *groupNameResolver {
	return &groupNameResolver{cache: make(map[string]string)}
}

// resolve looks up a group's real subject via whatsmeow, caching the
// result. Returns "" on parse/RPC failure so the caller can fall back to
// the stored chat name.
func (g *groupNameResolver) resolve(ctx context.Context, waClient *whatsmeow.Client, chatJID string) string {
	if waClient == nil {
		return ""
	}
	g.mu.Lock()
	if cached, ok := g.cache[chatJID]; ok {
		g.mu.Unlock()
		return cached
	}
	g.mu.Unlock()

	jid, err := types.ParseJID(chatJID)
	if err != nil {
		return ""
	}
	lookupCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	info, err := waClient.GetGroupInfo(lookupCtx, jid)
	if err != nil || info == nil || info.Name == "" {
		return ""
	}

	g.mu.Lock()
	g.cache[chatJID] = info.Name
	g.mu.Unlock()
	return info.Name
}

// pgImporterForSync returns the shared direct-Postgres importer, opening it
// lazily the first time it's requested. A blank import DB URI means the REST
// path is selected. A configured-but-broken URI is a sync error, not a REST
// fallback, because operators explicitly opted into direct DB import.
func (s *SyncService) pgImporterForSync(ctx context.Context) (*pgimport.Importer, error) {
	// Direct-Postgres import is driven by a single global DSN and is only valid
	// for the legacy/env config. Per-device configs use the REST import path.
	if !s.allowPgImport {
		return nil, nil
	}
	if strings.TrimSpace(config.ChatwootImportDBURI) == "" {
		return nil, nil
	}
	s.pgInitMu.Lock()
	defer s.pgInitMu.Unlock()
	if s.pgImporter != nil {
		return s.pgImporter, nil
	}
	imp, err := pgimport.New(ctx, pgimport.Config{
		DSN:       config.ChatwootImportDBURI,
		AccountID: config.ChatwootAccountID,
		InboxID:   config.ChatwootInboxID,
		// APIToken is forwarded so the importer can resolve the Chatwoot
		// agent that owns this token from the access_tokens table.
		// Outgoing imported messages get stamped with that agent so they
		// render with a name/avatar instead of as "Unknown sender".
		APIToken: config.ChatwootAPIToken,
	})
	if err != nil {
		return nil, fmt.Errorf("chatwoot pgimport: %w", err)
	}
	s.pgImporter = imp
	return s.pgImporter, nil
}

// GetProgress returns the current sync progress for a device
func (s *SyncService) GetProgress(deviceID string) *SyncProgress {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	if progress, ok := s.progressMap[deviceID]; ok {
		cloned := progress.Clone()
		return &cloned
	}
	return nil
}

// IsRunning returns true if a sync is currently running for the device
func (s *SyncService) IsRunning(deviceID string) bool {
	s.progressMu.RLock()
	defer s.progressMu.RUnlock()

	if progress, ok := s.progressMap[deviceID]; ok {
		return progress.IsRunning()
	}
	return false
}

// SyncHistory performs the initial message history sync to Chatwoot
func (s *SyncService) SyncHistory(ctx context.Context, deviceID string, waClient *whatsmeow.Client, opts SyncOptions) (*SyncProgress, error) {
	// Atomic check-and-set to prevent race condition
	progress := NewSyncProgress(deviceID)
	s.progressMu.Lock()
	if existing, ok := s.progressMap[deviceID]; ok && existing.IsRunning() {
		s.progressMu.Unlock()
		cloned := existing.Clone()
		return &cloned, fmt.Errorf("sync already in progress for device %s", deviceID)
	}
	s.progressMap[deviceID] = progress
	s.progressMu.Unlock()

	progress.SetRunning()

	// Per-run group-name cache — local so concurrent SyncHistory calls
	// for different devices don't share and invalidate each other's
	// entries.
	groupResolver := newGroupNameResolver()

	// Decide up front whether this run uses the direct-Postgres importer
	// or the REST path. The decision is logged
	// so operators can tell from the logs which path ran.
	importer, err := s.pgImporterForSync(ctx)
	if err != nil {
		progress.SetFailed(err)
		return progress, err
	}
	backend := "REST"
	if importer != nil {
		backend = "pgimport"
	}
	logrus.Infof("Chatwoot Sync: Starting history sync for device %s (backend=%s days=%d media=%v groups=%v)",
		deviceID, backend, opts.DaysLimit, opts.IncludeMedia, opts.IncludeGroups)

	// 1. Get all chats for this device
	chats, err := s.chatStorageRepo.GetChats(&domainChatStorage.ChatFilter{
		DeviceID: deviceID,
	})
	if err != nil {
		progress.SetFailed(err)
		return progress, fmt.Errorf("failed to get chats: %w", err)
	}

	progress.SetTotals(len(chats), 0)
	logrus.Infof("Chatwoot Sync: Found %d chats to sync", len(chats))

	// 2. Calculate time boundary
	sinceTime := time.Now().AddDate(0, 0, -opts.DaysLimit)

	// 3. Process each chat
	for _, chat := range chats {
		if err := ctx.Err(); err != nil {
			progress.SetFailed(err)
			return progress, err // Context cancelled
		}

		// status@broadcast, 0@s.whatsapp.net and @newsletter channels carry no
		// actionable conversation for an agent, and operators can ignore
		// additional JIDs (or whole address spaces) via CHATWOOT_IGNORE_JIDS.
		// Skipping at the chat level (rather than per-message) keeps the
		// totals honest in the progress tracker — the chat is excluded
		// entirely instead of counted as "synced 0".
		if utils.IsSystemBroadcastJID(chat.JID) || utils.IsNewsletterJID(chat.JID) || utils.MatchesIgnoredJID(chat.JID, config.ChatwootIgnoreJids) {
			logrus.Debugf("Chatwoot Sync: Skipping ignored chat %s", chat.JID)
			continue
		}

		progress.UpdateChat(chat.JID)

		// Resolve the real group subject before dispatching so both paths
		// write a meaningful name instead of the "Group 120363…@g.us"
		// fallback from sqlite_repository.go. Individual chats don't need
		// this — their stored name comes from the push-name pipeline.
		if strings.HasSuffix(chat.JID, "@g.us") {
			if resolved := groupResolver.resolve(ctx, waClient, chat.JID); resolved != "" {
				chat.Name = resolved
			}
		}

		var err error
		if importer != nil {
			err = s.syncChatPG(ctx, importer, deviceID, chat, sinceTime, waClient, opts, progress)
		} else {
			err = s.syncChat(ctx, deviceID, chat, sinceTime, waClient, opts, progress)
		}
		if err != nil {
			logrus.Errorf("Chatwoot Sync: Failed to sync chat %s: %v", chat.JID, err)
			progress.IncrementFailedChats()
			// Continue with other chats
		} else {
			progress.IncrementSyncedChats()
		}
	}

	progress.SetCompleted()
	logrus.Infof("Chatwoot Sync: Completed for device %s. Chats: %d (failed: %d), Messages: %d (failed: %d)",
		deviceID, progress.SyncedChats, progress.FailedChats, progress.SyncedMessages, progress.FailedMessages)

	return progress, nil
}

// syncChat syncs a single chat's messages to Chatwoot
func (s *SyncService) syncChat(
	ctx context.Context,
	deviceID string,
	chat *domainChatStorage.Chat,
	sinceTime time.Time,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	progress *SyncProgress,
) error {
	isGroup := strings.HasSuffix(chat.JID, "@g.us")

	// Skip groups if not configured
	if isGroup && !opts.IncludeGroups {
		logrus.Debugf("Chatwoot Sync: Skipping group %s (groups disabled)", chat.JID)
		return nil
	}

	logrus.Infof("Chatwoot Sync: Processing chat %s (%s)", chat.Name, chat.JID)

	// 1. Get messages since time boundary
	messages, err := s.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID:  deviceID,
		ChatJID:   chat.JID,
		StartTime: &sinceTime,
		Limit:     opts.MaxMessagesPerChat,
	})
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}

	if len(messages) == 0 {
		logrus.Debugf("Chatwoot Sync: No messages to sync for %s", chat.JID)
		return nil
	}

	progress.AddMessages(len(messages))
	logrus.Infof("Chatwoot Sync: Found %d messages for %s", len(messages), chat.JID)

	// 2. Drop the messages already linked to a Chatwoot message BEFORE resolving
	// the contact and conversation. FindOrCreateConversation reopens a resolved
	// conversation when ChatwootReopenConversation is set, so resolving one for a
	// chat with nothing new to post reopens it for no reason. History auto-sync
	// latches once per device per process, so every restart reopened every
	// resolved conversation in the inbox. Already-linked messages still count
	// as synced: the row is
	// present in Chatwoot, which is what the operator cares about.
	pending, err := s.pendingHistoryMessages(messages, progress, "Chatwoot Sync", chat.JID)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	// 4. Find or create contact and conversation. The conversation comes back
	// exactly as Chatwoot holds it: a resolved thread is NOT reopened here.
	conversation, err := s.findOrCreateHistoryConversation(ctx, chat, isGroup)
	if err != nil {
		return err
	}
	logrus.Debugf("Chatwoot Sync: Conversation ID: %d", conversation.ID)

	// 4a. Pre-arm the durable reopen intent BEFORE posting anything, when the
	// conversation is resolved and reopening is enabled. This, not the retry
	// around the queue write, is what makes the state machine recoverable:
	// nothing below this point (post, link, live reopen) is durable yet, so a
	// failure here can safely abort the whole chat and let the next sync
	// retry from scratch with no duplicate. Once a message posts and links,
	// that is no longer true -- the row already queued here is what a failed
	// live reopen falls back to instead of racing a fresh enqueue against an
	// already-irreversible state.
	preArmed := false
	if config.ChatwootReopenConversation && conversation.Status == "resolved" {
		if err := s.enqueueReopenIntent(ctx, deviceID, conversation, chat.JID, "reopen queued before posting into a resolved conversation"); err != nil {
			return fmt.Errorf("failed to queue reopen intent before posting: %w", err)
		}
		preArmed = true
	}

	// 5. Sync each message. Reopen the thread once, right after the first
	// message actually lands. A pending set whose every post fails -- media
	// that expired, a 4xx the retry policy gives up on, a link-store race --
	// then leaves a resolved thread resolved, instead of reopening it with
	// nothing added on every restart.
	reopened := false
	anyPosted := false
	var reopenErr error
	for i, msg := range pending {
		if err := ctx.Err(); err != nil {
			return err
		}

		posted, err := s.syncMessage(ctx, conversation.ID, msg, waClient, opts, isGroup)
		if err != nil {
			logrus.Warnf("Chatwoot Sync: Failed to sync message %s: %v", msg.ID, err)
			progress.IncrementFailedMessages()
			// Continue with other messages
		} else {
			progress.IncrementSyncedMessages()
		}
		if posted {
			anyPosted = true
		}
		// Keep trying on later posts if the toggle failed: a message has landed
		// in a thread the agent resolved, and once its link is stored nothing
		// on a later pass will look at it again.
		if posted && !reopened {
			reopened, reopenErr = s.reopenHistoryConversation(ctx, conversation)
		}

		// Rate limiting: pause between batches
		if i > 0 && i%opts.BatchSize == 0 {
			time.Sleep(opts.DelayBetweenBatches)
		}
	}

	if anyPosted && !reopened {
		// Every reopen attempt this pass failed. The posted messages are linked
		// now, so no later pass will look at them again -- the reopen has to
		// outlive this run on its own.
		s.persistReopenIntent(ctx, deviceID, conversation, chat.JID, reopenErr)
	} else if preArmed && !anyPosted {
		// Nothing posted this pass (every message failed, or a link-store race
		// took every one of them), but the pre-arm above already queued a live
		// intent. Void it so the worker does not reopen a thread nothing was
		// added to.
		s.voidReopenIntent(ctx, deviceID, conversation, chat.JID)
	}
	return nil
}

// syncChatPG syncs a chat's history to Chatwoot via the direct-Postgres
// importer. It mirrors syncChat's preprocessing (filtering groups, time
// window, sort order) but delegates the actual writes to pgimport, which
// uses a single transaction per chat and preserves original timestamps.
//
// Media is normally represented by text placeholders because direct-DB import
// does not touch Chatwoot's ActiveStorage layer. When
// ChatwootImportMediaWithREST is enabled, downloadable media rows are first
// uploaded through Chatwoot REST with the same source_id; the following DB
// import then skips those REST-created rows idempotently.
//
// pgimport's own reopen (inside ImportChat) needs no pre-arm: it runs in the
// same DB transaction as the message writes it follows and only commits once,
// so a failed reopen there rolls the whole write back instead of leaving a
// posted-and-linked message behind with nothing durable pointing at an owed
// reopen. The REST media pre-pass below is a separate write path (Chatwoot
// REST, not this transaction) and does need its own pre-arm; see
// restMediaPrePass.
func (s *SyncService) syncChatPG(
	ctx context.Context,
	importer *pgimport.Importer,
	deviceID string,
	chat *domainChatStorage.Chat,
	sinceTime time.Time,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	progress *SyncProgress,
) error {
	isGroup := strings.HasSuffix(chat.JID, "@g.us")
	if isGroup && !opts.IncludeGroups {
		logrus.Debugf("Chatwoot Sync: Skipping group %s (groups disabled)", chat.JID)
		return nil
	}

	logrus.Infof("Chatwoot pgimport: Processing chat %s (%s)", chat.Name, chat.JID)

	messages, err := s.chatStorageRepo.GetMessages(&domainChatStorage.MessageFilter{
		DeviceID:  deviceID,
		ChatJID:   chat.JID,
		StartTime: &sinceTime,
		Limit:     opts.MaxMessagesPerChat,
	})
	if err != nil {
		return fmt.Errorf("failed to get messages: %w", err)
	}
	if len(messages) == 0 {
		logrus.Debugf("Chatwoot pgimport: No messages to sync for %s", chat.JID)
		return nil
	}

	progress.AddMessages(len(messages))
	logrus.Infof("Chatwoot pgimport: Found %d messages for %s", len(messages), chat.JID)

	// Drop the messages already in Chatwoot before importing anything. This
	// is the cheap first line: it spares the importer a transaction per chat
	// with nothing new. It is not the only guard, and it cannot be, because
	// the local link table is written only after ImportChat commits -- a crash
	// in between leaves a message in Chatwoot with no local link, and this
	// filter then reports it as pending. The importer therefore makes its own
	// reopen decision after the write: findOrCreateConversation never changes
	// status, and ImportChat reopens only when MessagesWrote > 0, so a replay
	// that skips every row leaves a resolved thread resolved even when this
	// filter let it through. The skipped rows still come back as links, which
	// storeChatwootImportLinks uses to repair the local table. The REST media
	// pre-pass below keeps the same invariant on its own side: it resolves
	// without reopening and toggles only after an attachment actually posts.
	//
	// Already-linked messages still count as synced: the row is present in
	// Chatwoot, which is what the operator cares about. ImportChat only sees
	// the pending ones, so its own wrote/skipped totals do not double-count
	// what was added here.
	pending, err := s.pendingHistoryMessages(messages, progress, "Chatwoot pgimport", chat.JID)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	// Use a display name that is never the "Group <jid>" fallback from
	// sqlite_repository.go. If the stored name still starts with "Group "
	// followed by the JID user portion, treat it as unset and let the
	// importer use the phone number / JID as last resort.
	chatName := chat.Name
	if isGroup && strings.HasPrefix(chatName, "Group ") {
		chatName = ""
	}

	s.restMediaPrePass(ctx, deviceID, chat, pending, waClient, opts, isGroup)

	result, err := importer.ImportChat(ctx, pgimport.ImportChatRequest{
		ChatJID:  chat.JID,
		ChatName: chatName,
		Messages: pending,
	})
	if err != nil {
		// On a fatal tx-level error, count every message the import was given
		// as failed so the progress totals stay honest. The already-linked ones
		// were counted as synced above and did not reach the importer.
		progress.AddFailedMessages(len(pending))
		return err
	}
	if err := s.storeChatwootImportLinks(result); err != nil {
		return err
	}

	// Idempotent skips are recorded as "synced" for UI purposes — the row
	// is present in Chatwoot, which is what the operator cares about.
	progress.AddSyncedMessages(result.MessagesWrote + result.MessagesSkipped)
	progress.AddFailedMessages(result.MessagesFailed)

	logrus.Infof("Chatwoot pgimport: %s wrote=%d skipped=%d failed=%d",
		chat.JID, result.MessagesWrote, result.MessagesSkipped, result.MessagesFailed)
	return nil
}

func hasDownloadableChatwootMedia(msg *domainChatStorage.Message) bool {
	return msg != nil && msg.MediaType != "" && utils.ResolveMediaDirectPath(msg.DirectPath, msg.URL) != "" && len(msg.MediaKey) > 0
}

func chatwootRESTMediaCandidates(messages []*domainChatStorage.Message, opts SyncOptions) []*domainChatStorage.Message {
	if !config.ChatwootImportMediaWithREST || !opts.IncludeMedia {
		return nil
	}
	candidates := make([]*domainChatStorage.Message, 0)
	for _, msg := range messages {
		if hasDownloadableChatwootMedia(msg) {
			candidates = append(candidates, msg)
		}
	}
	return candidates
}

// chatwootLinkFor returns the local link that says msg already exists in
// Chatwoot, or nil when there is none. It is the single definition of "already
// linked" shared by the history prefilter and the per-message guard in
// syncMessageWithOptions, so the two can never disagree on what counts.
func (s *SyncService) chatwootLinkFor(msg *domainChatStorage.Message) (*domainChatStorage.ChatwootMessageLink, error) {
	if msg == nil || msg.ID == "" || msg.DeviceID == "" || s.chatStorageRepo == nil {
		return nil, nil
	}
	existing, err := s.chatStorageRepo.GetChatwootMessageLinkByWhatsAppID(msg.DeviceID, msg.ID)
	if err != nil {
		return nil, fmt.Errorf("failed to look up message link for %s: %w", msg.ID, err)
	}
	if existing == nil || existing.ChatwootMessageID == 0 {
		return nil, nil
	}
	return existing, nil
}

// pendingHistoryMessages returns the messages a history sync still has to put
// into Chatwoot, oldest first, and records the rest on progress. It lets a
// caller decide whether a chat needs a conversation at all before resolving
// one, since resolving is not side-effect free.
//
// Already-linked messages count as synced: the row is present in Chatwoot,
// which is what the operator cares about. A lookup failure aborts the chat
// instead of guessing -- treating an unreadable link as pending would let the
// caller resolve (and reopen) a conversation for a message that is then found
// linked and never posted. The links confirmed before the failure still count
// as synced, and the ones left unclassified count as failed, so the totals
// stay honest for a chat that is recorded as failed. An empty result with a
// nil error means there is nothing to post.
func (s *SyncService) pendingHistoryMessages(messages []*domainChatStorage.Message, progress *SyncProgress, logPrefix, chatJID string) ([]*domainChatStorage.Message, error) {
	pending := make([]*domainChatStorage.Message, 0, len(messages))
	alreadyLinked := 0
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		link, err := s.chatwootLinkFor(msg)
		if err != nil {
			progress.AddSyncedMessages(alreadyLinked)
			progress.AddFailedMessages(len(messages) - alreadyLinked)
			return nil, err
		}
		if link != nil {
			alreadyLinked++
			continue
		}
		pending = append(pending, msg)
	}
	progress.AddSyncedMessages(alreadyLinked)
	if len(pending) == 0 {
		logrus.Debugf("%s: All %d messages for %s are already in Chatwoot; leaving the conversation untouched", logPrefix, alreadyLinked, chatJID)
		return nil, nil
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].Timestamp.Before(pending[j].Timestamp)
	})
	return pending, nil
}

// findOrCreateHistoryConversation resolves the contact and the conversation a
// history sync should post into, without changing the conversation's status.
// It mirrors client.FindOrCreateConversation -- prefer an open thread, else the
// latest one when reopening is enabled, else create -- but leaves the reopen to
// reopenHistoryConversation, which the caller invokes only after a message has
// actually landed. The live inbound path keeps the composite client call: there
// a message is guaranteed to follow, so reopening up front is correct.
func (s *SyncService) findOrCreateHistoryConversation(ctx context.Context, chat *domainChatStorage.Chat, isGroup bool) (*Conversation, error) {
	contactName := chat.Name
	if contactName == "" {
		contactName = utils.ExtractPhoneFromJID(chat.JID)
	}

	var contact *Contact
	err := retrySyncOp(ctx, 3, func() error {
		var createErr error
		contact, createErr = s.client.FindOrCreateContact(contactName, chat.JID, isGroup)
		return createErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create contact: %w", err)
	}

	var conversation *Conversation
	err = retrySyncOp(ctx, 3, func() error {
		items, listErr := s.client.listContactConversations(contact.ID)
		if listErr != nil {
			return listErr
		}
		if open := selectOpenConversation(items, s.client.InboxID, contact.ID); open != nil {
			conversation = open
			return nil
		}
		if config.ChatwootReopenConversation {
			if latest := selectLatestConversation(items, s.client.InboxID, contact.ID); latest != nil {
				conversation = latest
				return nil
			}
		}
		var createErr error
		conversation, createErr = s.client.CreateConversation(contact.ID, chat.JID)
		return createErr
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create conversation: %w", err)
	}
	return conversation, nil
}

// reopenHistoryConversation flips a resolved conversation back to the
// new-message status, the REST counterpart of pgimport's reopenConversation.
// Callers invoke it after a message of a history sync has posted, so a thread
// the agent resolved is reopened only when something was actually added.
//
// It reports whether the thread is now in the wanted state -- true when there
// was nothing to reopen or the toggle succeeded, false plus the failure when
// the toggle failed after retries. Unlike the Postgres path, where a failed
// reopen rolls the whole import back, a REST post is already durable by the
// time this runs, so the caller must keep retrying on its later posts rather
// than give up.
func (s *SyncService) reopenHistoryConversation(ctx context.Context, conversation *Conversation) (bool, error) {
	if conversation == nil || !config.ChatwootReopenConversation || conversation.Status != "resolved" {
		return true, nil
	}
	target := conversationStatusForNew()
	err := retrySyncOp(ctx, 3, func() error {
		return s.client.ToggleConversationStatus(conversation.ID, target)
	})
	if err != nil {
		logrus.Warnf("Chatwoot Sync: failed to reopen conversation %d after posting: %v", conversation.ID, err)
		return false, err
	}
	conversation.Status = target
	return true, nil
}

// ReopenForwardEventName is the chatwoot_forward_queue event name carrying a
// history-sync reopen that could not be delivered. Reusing that queue gives the
// intent the retry machinery the message forwards already have -- durable rows,
// exponential backoff, one worker -- instead of a second table and a second
// loop for a single integer.
const ReopenForwardEventName = "chatwoot.conversation.reopen"

// ReopenIntent is the payload of a ReopenForwardEventName queue row: everything
// the worker needs to finish the toggle in a later process.
//
// AccountID pins the intent to the Chatwoot account it was queued for, since
// conversation ids are only unique within an account and a device can be
// rebound to another one. EnqueuedAt (epoch seconds) is what makes the intent
// falsifiable: a conversation whose last activity is newer than it was resolved
// again after the sync gave up, so the queued reopen no longer describes
// anything true and must be dropped rather than replayed.
type ReopenIntent struct {
	ConversationID int    `json:"conversation_id"`
	AccountID      int    `json:"account_id"`
	TargetStatus   string `json:"target_status"`
	ChatJID        string `json:"chat_jid"`
	EnqueuedAt     int64  `json:"enqueued_at"`
}

// ReopenIntentQueueKey is the wa_message_id a reopen intent is stored under.
// That column is the queue's dedup key (device_id, event_name, wa_message_id),
// so keying it by conversation collapses repeated failures for one thread into
// a single row instead of one per posted message. The "conversation:" prefix
// keeps the value out of the WhatsApp message id space.
func ReopenIntentQueueKey(conversationID int) string {
	return fmt.Sprintf("conversation:%d", conversationID)
}

// reopenIntentEnqueueAttempts bounds the retries around the queue write in
// persistReopenIntent. The message that made this necessary is already
// durable (posted and linked); the queue row is the only thing standing
// between that state and a conversation stuck resolved forever, so a single
// failed INSERT (SQLite lock contention, a momentary disk hiccup) must not be
// allowed to make that terminal the way one failed toggle already isn't.
const reopenIntentEnqueueAttempts = 3

// enqueueReopenIntent writes (or refreshes) the durable reopen-intent row for
// conversation, retrying the write itself against transient local-storage
// failures. lastError is stored on the row for operators reading the queue;
// it does not have to name a Chatwoot failure -- the pre-arm call in syncChat
// passes a fixed description since no reopen attempt has happened yet.
//
// Re-enqueueing an existing row rewrites its payload (the queue's unique key
// is device+event+conversation), so a later call refreshes EnqueuedAt and the
// window the worker allows itself, and also updates TargetStatus/LastError to
// the latest call's values.
func (s *SyncService) enqueueReopenIntent(ctx context.Context, deviceID string, conversation *Conversation, chatJID, lastError string) error {
	return s.writeReopenIntentRow(ctx, deviceID, conversation, chatJID, lastError, time.Now().Unix())
}

// voidReopenIntent overwrites a pre-armed reopen-intent row so it can never
// fire. It exists for the one case the pre-arm in syncChat/restMediaPrePass
// creates on its own: the pre-arm runs before the posting loop, so a pass
// where every post then fails would otherwise leave a real, live intent
// behind for a conversation nothing was actually added to -- reopening it on
// the worker's next pass would be exactly the "resolved thread reopened with
// nothing new inside" regression pendingHistoryMessages/
// reopenHistoryConversation exist to prevent.
//
// It reuses replayChatwootReopenIntent's existing malformed-intent guard
// (EnqueuedAt <= 0) rather than adding a second cancellation state: that path
// already drops the row outright, with no Chatwoot call, so voiding here is
// exactly "make the row look like the thing the worker already ignores."
// Best-effort: if a message posts later and this row gets refreshed by
// persistReopenIntent or a future pre-arm, the void is overwritten by a real
// EnqueuedAt, same as any other re-enqueue.
func (s *SyncService) voidReopenIntent(ctx context.Context, deviceID string, conversation *Conversation, chatJID string) {
	if conversation == nil {
		return
	}
	if err := s.writeReopenIntentRow(ctx, deviceID, conversation, chatJID, "voided: nothing posted this pass", 0); err != nil {
		logrus.Errorf("Chatwoot Sync: failed to void the pre-armed reopen intent for conversation %d after nothing posted; it may still reopen the thread with nothing added: %v", conversation.ID, err)
	}
}

// writeReopenIntentRow is the shared write behind enqueueReopenIntent (a real,
// live intent) and voidReopenIntent (a deliberately inert one, enqueuedAt=0).
func (s *SyncService) writeReopenIntentRow(ctx context.Context, deviceID string, conversation *Conversation, chatJID, lastError string, enqueuedAt int64) error {
	if conversation == nil {
		return nil
	}
	if s.chatStorageRepo == nil || s.client == nil || deviceID == "" {
		return fmt.Errorf("cannot queue reopen intent for conversation %d (device=%q storage=%v client=%v)",
			conversation.ID, deviceID, s.chatStorageRepo != nil, s.client != nil)
	}

	payload, err := json.Marshal(ReopenIntent{
		ConversationID: conversation.ID,
		AccountID:      s.client.AccountID,
		TargetStatus:   conversationStatusForNew(),
		ChatJID:        chatJID,
		EnqueuedAt:     enqueuedAt,
	})
	if err != nil {
		return fmt.Errorf("failed to serialize reopen intent for conversation %d: %w", conversation.ID, err)
	}

	event := &domainChatStorage.ChatwootForwardEvent{
		DeviceID:          deviceID,
		EventName:         ReopenForwardEventName,
		WhatsAppMessageID: ReopenIntentQueueKey(conversation.ID),
		PayloadJSON:       string(payload),
		LastError:         lastError,
		// One minute is the first delay the forward queue uses; the worker
		// takes over the doubling from there.
		NextAttemptAt: time.Now().Add(time.Minute),
	}
	return retrySyncOp(ctx, reopenIntentEnqueueAttempts, func() error {
		return s.chatStorageRepo.EnqueueChatwootForwardEvent(event)
	})
}

// persistReopenIntent refreshes the reopen-intent row after a live reopen
// attempt failed post-post, so the Chatwoot forward retry worker finishes the
// toggle later without reposting anything.
//
// By the time this runs, the row it refreshes was normally already written by
// the pre-arm call in syncChat/restMediaPrePass before anything was posted --
// that pre-arm, not this call, is what makes the state recoverable: it exists
// before the post, so nothing here can be the only durable trace of an owed
// reopen. This call still retries the write and still logs loudly on failure,
// because a chat can reach this point without a pre-arm (e.g. the conversation
// was open when posting started and only became resolved through the message
// itself, or an older row predates this pre-arm existing), and because a
// refreshed EnqueuedAt/LastError is worth having even when it is not the only
// copy.
func (s *SyncService) persistReopenIntent(ctx context.Context, deviceID string, conversation *Conversation, chatJID string, reopenErr error) {
	if conversation == nil {
		return
	}

	lastError := "reopen after history post failed"
	if reopenErr != nil {
		lastError = reopenErr.Error()
	}

	if err := s.enqueueReopenIntent(ctx, deviceID, conversation, chatJID, lastError); err != nil {
		logrus.Errorf("Chatwoot Sync: posted into conversation %d for %s but could not reopen it, and refreshing the retry queue failed after %d attempts (%v); a pre-armed row queued before posting may still cover it",
			conversation.ID, chatJID, reopenIntentEnqueueAttempts, err)
		return
	}
	logrus.Warnf("Chatwoot Sync: posted into conversation %d for %s but could not reopen it; queued a durable reopen retry", conversation.ID, chatJID)
}

// restMediaPrePass posts a chat's media through the REST API before the
// Postgres importer runs, so the attachment lands on the message pgimport is
// about to write. Callers pass the messages already known to be pending; the
// pass does not re-check links.
//
// It resolves the conversation without reopening it and reopens only after an
// attachment has actually posted. Media that can never land -- expired on the
// WhatsApp side, or rejected by Chatwoot -- therefore never reopens a thread
// the agent resolved, on this run or any later one.
func (s *SyncService) restMediaPrePass(
	ctx context.Context,
	deviceID string,
	chat *domainChatStorage.Chat,
	messages []*domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) {
	mediaMessages := chatwootRESTMediaCandidates(messages, opts)
	if len(mediaMessages) == 0 {
		return
	}

	conversation, err := s.findOrCreateHistoryConversation(ctx, chat, isGroup)
	if err != nil {
		logrus.Warnf("Chatwoot pgimport: REST media pre-pass skipped for %s: %v", chat.JID, err)
		return
	}

	// Pre-arm the durable reopen intent before posting any attachment, for the
	// same reason syncChat does: nothing below this point is durable yet, so a
	// pre-arm failure can skip the pre-pass entirely (the importer that follows
	// still runs, and its own transactional reopen is unaffected) instead of
	// risking a posted-and-linked attachment with no durable trace that a
	// reopen is owed.
	preArmed := false
	if config.ChatwootReopenConversation && conversation.Status == "resolved" {
		if err := s.enqueueReopenIntent(ctx, deviceID, conversation, chat.JID, "reopen queued before posting into a resolved conversation"); err != nil {
			logrus.Warnf("Chatwoot pgimport: REST media pre-pass skipped for %s: failed to queue reopen intent before posting: %v", chat.JID, err)
			return
		}
		preArmed = true
	}

	anyPosted, reopened, reopenErr := s.syncHybridMediaMessagesREST(ctx, conversation, mediaMessages, waClient, opts, isGroup)
	if anyPosted && !reopened {
		// The importer that follows only reopens on its own writes, and these
		// rows will be skipped there, so nothing downstream would retry.
		s.persistReopenIntent(ctx, deviceID, conversation, chat.JID, reopenErr)
	} else if preArmed && !anyPosted {
		// No attachment posted this pass; void the pre-armed row for the same
		// reason syncChat does -- nothing was added, so nothing should reopen.
		s.voidReopenIntent(ctx, deviceID, conversation, chat.JID)
	}
}

// syncHybridMediaMessagesREST posts each media message and, once one has
// landed, reopens the conversation -- retrying on every later post until it
// succeeds, exactly as syncChat does. It reports whether anything posted,
// whether the thread ended up reopened (or needed no reopening), and the last
// toggle failure, which the caller records on the durable retry.
func (s *SyncService) syncHybridMediaMessagesREST(
	ctx context.Context,
	conversation *Conversation,
	messages []*domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) (anyPosted, reopened bool, reopenErr error) {
	for _, msg := range messages {
		if err := ctx.Err(); err != nil {
			return anyPosted, reopened, reopenErr
		}
		posted, err := s.syncMessageWithOptions(ctx, conversation.ID, msg, waClient, opts, isGroup, true)
		if err != nil {
			logrus.Warnf("Chatwoot pgimport: REST media pre-pass failed for message %s: %v", msg.ID, err)
		}
		if posted {
			anyPosted = true
			if !reopened {
				reopened, reopenErr = s.reopenHistoryConversation(ctx, conversation)
			}
		}
	}
	return anyPosted, reopened, reopenErr
}

func (s *SyncService) storeChatwootImportLinks(result *pgimport.ImportResult) error {
	if s.chatStorageRepo == nil || result == nil {
		return nil
	}
	for i := range result.Links {
		link := result.Links[i]
		if err := s.chatStorageRepo.UpsertChatwootMessageLink(&link); err != nil {
			return fmt.Errorf("failed to store chatwoot import link for %s: %w", link.WhatsAppMessageID, err)
		}
	}
	return nil
}

// syncMessage syncs a single message to Chatwoot
func (s *SyncService) syncMessage(
	ctx context.Context,
	conversationID int,
	msg *domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
) (bool, error) {
	return s.syncMessageWithOptions(ctx, conversationID, msg, waClient, opts, isGroup, false)
}

func (s *SyncService) syncMessageWithOptions(
	ctx context.Context,
	conversationID int,
	msg *domainChatStorage.Message,
	waClient *whatsmeow.Client,
	opts SyncOptions,
	isGroup bool,
	requireMediaAttachment bool,
) (posted bool, err error) {
	if existing, err := s.chatwootLinkFor(msg); err != nil {
		return false, err
	} else if existing != nil {
		logrus.Debugf("Chatwoot Sync: Skipping already-linked message %s -> %d", msg.ID, existing.ChatwootMessageID)
		return false, nil
	}

	// Determine message type: "incoming" or "outgoing"
	messageType := "incoming"
	if msg.IsFromMe {
		messageType = "outgoing"
	}

	// Build content
	content := msg.Content
	if content == "" && msg.MediaType != "" {
		content = fmt.Sprintf("[%s]", msg.MediaType) // Placeholder for media-only
	}

	// For group messages, add sender info so agents can tell participants apart
	if isGroup && !msg.IsFromMe && msg.Sender != "" {
		senderName := utils.ExtractPhoneFromJID(msg.Sender)
		content = fmt.Sprintf("%s: %s", senderName, content)
	}

	var attachments []string

	// Handle media if enabled and present
	if opts.IncludeMedia && msg.MediaType != "" && msg.URL != "" && len(msg.MediaKey) > 0 {
		filePath, err := s.fetchMedia(ctx, msg, waClient)
		if err != nil {
			if requireMediaAttachment {
				return false, fmt.Errorf("failed to download required media: %w", err)
			}
			logrus.Debugf("Chatwoot Sync: Failed to download media for message %s: %v", msg.ID, err)
			// Continue without media - it might be expired
			content += " [media unavailable]"
		} else if filePath != "" {
			attachments = append(attachments, filePath)
		}
	}
	if requireMediaAttachment && len(attachments) == 0 {
		return false, fmt.Errorf("required media attachment is unavailable")
	}

	// Send to Chatwoot with retry on transient errors (429, 5xx). Register
	// the returned ID in the dedup cache so the resulting webhook event is
	// recognized as "ours" and not forwarded back to WhatsApp.
	// Stamp source_id = WAID:<id> so the REST-imported row shares the importer's
	// dedup key and gives later replies a stable thread anchor.
	var msgOpts MessageOptions
	if msg.ID != "" {
		msgOpts.SourceID = "WAID:" + msg.ID
	}

	var msgID int
	err = retrySyncOp(ctx, 3, func() error {
		var createErr error
		msgID, createErr = s.client.CreateMessage(conversationID, content, messageType, attachments, msgOpts)
		return createErr
	})

	for _, fp := range attachments {
		if err := os.Remove(fp); err != nil {
			logrus.Debugf("Chatwoot Sync: Failed to remove temp file %s: %v", fp, err)
		}
	}

	if err != nil {
		return false, fmt.Errorf("failed to create message: %w", err)
	}

	// From here on a message exists in Chatwoot whatever happens to the link
	// store below; callers use the returned true to decide whether to reopen
	// the thread.
	MarkMessageAsSent(s.client.AccountID, msgID)
	if msgID != 0 && msg.ID != "" && msg.DeviceID != "" && s.chatStorageRepo != nil {
		if err := s.chatStorageRepo.UpsertChatwootMessageLink(&domainChatStorage.ChatwootMessageLink{
			DeviceID:                     msg.DeviceID,
			WhatsAppMessageID:            msg.ID,
			WhatsAppChatJID:              msg.ChatJID,
			ChatwootMessageID:            msgID,
			ChatwootConversationID:       conversationID,
			ChatwootInboxID:              s.client.InboxID,
			ChatwootContactInboxSourceID: msg.ChatJID,
			SourceID:                     msgOpts.SourceID,
			Direction:                    messageType,
			IsRead:                       false,
			// Scope the link to this service's Chatwoot account/config so reverse
			// routing stays account-scoped. Without this, a REST history-sync link
			// would default to account 0 and match the legacy wildcard in
			// GetLatestChatwootMessageLinkByConversation, allowing cross-account
			// mis-routing when conversation ids collide across accounts.
			ChatwootConfigID:  s.configID,
			ChatwootAccountID: s.client.AccountID,
		}); err != nil {
			return true, fmt.Errorf("failed to store chatwoot message link: %w", err)
		}
	}

	return true, nil
}

// fetchMedia routes through the test seam when one is set, else downloads.
func (s *SyncService) fetchMedia(ctx context.Context, msg *domainChatStorage.Message, waClient *whatsmeow.Client) (string, error) {
	if s.mediaDownloader != nil {
		return s.mediaDownloader(ctx, msg, waClient)
	}
	return s.downloadMedia(ctx, msg, waClient)
}

// downloadMedia downloads media for a message and returns the temp file path
func (s *SyncService) downloadMedia(ctx context.Context, msg *domainChatStorage.Message, waClient *whatsmeow.Client) (string, error) {
	directPath := utils.ResolveMediaDirectPath(msg.DirectPath, msg.URL)
	if directPath == "" || len(msg.MediaKey) == 0 {
		return "", fmt.Errorf("missing media direct path or key")
	}

	if waClient == nil {
		return "", fmt.Errorf("WhatsApp client not available")
	}

	downloadable, err := utils.BuildDownloadableMessage(
		msg.MediaType,
		msg.URL,
		directPath,
		msg.Filename,
		msg.MediaKey,
		msg.FileSHA256,
		msg.FileEncSHA256,
		msg.FileLength,
	)
	if err != nil {
		return "", fmt.Errorf("unsupported media type: %s", msg.MediaType)
	}

	// Download
	data, err := waClient.Download(ctx, downloadable)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}

	// Write to temp file
	ext := getExtensionForMediaType(msg.MediaType, msg.Filename)
	tmpFile, err := os.CreateTemp("", fmt.Sprintf("chatwoot-sync-*%s", ext))
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.Write(data); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("failed to write media: %w", err)
	}

	return tmpFile.Name(), nil
}

// getExtensionForMediaType returns the file extension for a media type
func getExtensionForMediaType(mediaType, filename string) string {
	if filename != "" {
		if ext := filepath.Ext(filename); ext != "" {
			return ext
		}
	}
	switch mediaType {
	case "image":
		return ".jpg"
	case "video":
		return ".mp4"
	case "audio", "ptt":
		return ".oga"
	case "document":
		return ".bin"
	case "sticker":
		return ".webp"
	default:
		return ""
	}
}

// retrySyncOp retries fn up to maxAttempts times with exponential backoff
// (1s, 2s, 4s). Retries only transient errors — Retryable() returns true
// for network/IO failures and for HTTP 429 / 5xx responses, and false for
// 4xx validation errors so we don't hammer Chatwoot on misconfiguration.
func retrySyncOp(ctx context.Context, maxAttempts int, fn func() error) error {
	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		lastErr = fn()
		if lastErr == nil {
			return nil
		}
		if !Retryable(lastErr) {
			return lastErr
		}
		if attempt < maxAttempts-1 {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			logrus.Debugf("Chatwoot Sync: retry attempt %d/%d after %v: %v", attempt+1, maxAttempts, backoff, lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}
	return lastErr
}

// Per-device sync services for REST endpoints and auto-sync. Each device-config
// gets its own service bound to that device's Chatwoot client; the legacy/env
// config uses the empty key. Progress is still tracked per device inside each
// service, but the *client* must be per-config so a sync for one device targets
// the right Chatwoot account.
const legacySyncServiceKey = ""

var (
	syncServices   = make(map[string]*SyncService)
	syncServicesMu sync.RWMutex
)

// GetSyncServiceForDevice returns (creating on first use) the sync service for a
// device key, bound to the given client. allowPgImport enables direct-Postgres
// import and must be true only for the legacy/env config.
//
// A cached service is reused only while its client still addresses the same
// destination with the same credentials. When a per-device config is rewritten
// (token rotation, routing edit) the registry hands back a freshly built client,
// so the cached service is rebuilt rather than continuing to use the stale one
// until process restart. The previous service is left for any in-flight sync to
// finish on; per-device services hold no pooled resources to close. Its
// progress entries are carried over to the replacement so an in-flight run
// stays visible — and keeps blocking a concurrent second run — across the
// rebuild (SyncProgress values are pointers with their own lock, so the old
// run keeps updating the same entries the new service reports).
func GetSyncServiceForDevice(
	key string,
	client *Client,
	chatStorageRepo domainChatStorage.IChatStorageRepository,
	allowPgImport bool,
	configID int64,
) *SyncService {
	syncServicesMu.RLock()
	if s, ok := syncServices[key]; ok && sameChatwootClient(s.client, client) {
		syncServicesMu.RUnlock()
		return s
	}
	syncServicesMu.RUnlock()

	syncServicesMu.Lock()
	defer syncServicesMu.Unlock()
	if s, ok := syncServices[key]; ok && sameChatwootClient(s.client, client) {
		return s
	}
	s := NewSyncService(client, chatStorageRepo)
	s.allowPgImport = allowPgImport
	s.configID = configID
	if old, ok := syncServices[key]; ok {
		old.progressMu.RLock()
		maps.Copy(s.progressMap, old.progressMap)
		old.progressMu.RUnlock()
	}
	syncServices[key] = s
	return s
}

// sameChatwootClient reports whether two clients target the same Chatwoot
// destination with the same credentials. A nil client only matches another nil.
func sameChatwootClient(a, b *Client) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.BaseURL == b.BaseURL &&
		a.APIToken == b.APIToken &&
		a.AccountID == b.AccountID &&
		a.InboxID == b.InboxID
}

// LookupSyncServiceForDevice returns the existing sync service for a key without
// creating one (nil if none has run yet). Used by status queries.
func LookupSyncServiceForDevice(key string) *SyncService {
	syncServicesMu.RLock()
	defer syncServicesMu.RUnlock()
	return syncServices[key]
}

// CloseAllSyncServices closes every per-device sync service, releasing any
// direct-Postgres importer pools. Returns the first error encountered.
func CloseAllSyncServices() error {
	syncServicesMu.Lock()
	defer syncServicesMu.Unlock()
	var firstErr error
	for key, s := range syncServices {
		if err := s.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
		delete(syncServices, key)
	}
	return firstErr
}

// SyncServiceKeyFor returns the map key for a resolved config: the legacy key
// for the env config, else the device id. REST and auto-sync share it so a
// device's running/progress state is found under one key.
func SyncServiceKeyFor(rc *ResolvedConfig) string {
	if rc == nil || rc.ConfigID == 0 {
		return legacySyncServiceKey
	}
	return rc.DeviceID
}

// Close releases resources held by the sync service, including the optional
// direct-Postgres importer pool. Safe to call even when pgImporter is nil.
func (s *SyncService) Close() error {
	if s.pgImporter != nil {
		return s.pgImporter.Close()
	}
	return nil
}

// autoSyncTriggered latches per storage-JID so history auto-sync runs at most
// once per device per process. events.Connected fires on every reconnect, and
// SyncHistory's in-flight guard only blocks *concurrent* runs — without this
// latch a reconnect after a completed sync would re-import history (harmless for
// the idempotent pgimport path, but the REST path would duplicate messages).
var autoSyncTriggered sync.Map

// TriggerAutoSync starts a one-time history sync for a freshly connected device
// when CHATWOOT_IMPORT_MESSAGES is enabled. It is safe to call on every connect
// event: it self-guards on configuration, requires a logged-in client (so the
// storage JID is available), and latches per device so it runs only once per
// process. The sync runs in the background and never blocks the caller.
func TriggerAutoSync(chatStorageRepo domainChatStorage.IChatStorageRepository, waClient *whatsmeow.Client) {
	if !config.ChatwootEnabled || !config.ChatwootImportMessages {
		return
	}

	// Chats are stored under the full WhatsApp JID, so a logged-in client is
	// required to resolve the storage device ID. Before login there is nothing
	// to sync; a later connect (post-login) retries.
	if waClient == nil || waClient.Store == nil || waClient.Store.ID == nil {
		return
	}
	storageDeviceID := waClient.Store.ID.ToNonAD().String()
	if storageDeviceID == "" {
		return
	}

	// Resolve the per-device Chatwoot client by the storage JID. In legacy mode
	// (no per-device configs) this returns the env client.
	reg := GetClientRegistry()
	if reg == nil {
		logrus.Warn("Chatwoot Sync: Auto-sync skipped - client registry not initialized")
		return
	}
	rc, err := reg.Resolve(storageDeviceID)
	if err != nil {
		logrus.Warnf("Chatwoot Sync: Auto-sync skipped - resolve device %s: %v", storageDeviceID, err)
		return
	}
	if rc == nil || rc.Client == nil || !rc.Client.IsConfigured() {
		// No usable config yet (provisioning pending, or device unmapped); don't
		// consume the latch so a later connect can retry once configured.
		logrus.Warnf("Chatwoot Sync: Auto-sync skipped - no Chatwoot config for device %s", storageDeviceID)
		return
	}

	if _, loaded := autoSyncTriggered.LoadOrStore(storageDeviceID, struct{}{}); loaded {
		return // already triggered this process for this device
	}

	syncService := GetSyncServiceForDevice(SyncServiceKeyFor(rc), rc.Client, chatStorageRepo, rc.ConfigID == 0, rc.ConfigID)

	go func() {
		opts := DefaultSyncOptions()
		opts.DaysLimit = config.ChatwootDaysLimitImportMessages

		logrus.Infof("Chatwoot Sync: Auto-sync triggered for device %s", storageDeviceID)

		_, err := syncService.SyncHistory(context.Background(), storageDeviceID, waClient, opts)
		if err != nil {
			logrus.Errorf("Chatwoot Sync: Auto-sync failed for device %s: %v", storageDeviceID, err)
		}
	}()
}
