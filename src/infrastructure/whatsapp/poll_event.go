package whatsapp

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

const (
	pollResolutionResolved          = "resolved"
	pollResolutionPartiallyResolved = "partially_resolved"
	pollResolutionDefinitionMissing = "definition_missing"
	pollResolutionDecryptFailed     = "decrypt_failed"
)

type pollDefinitionStore interface {
	UpsertPollDefinition(definition *domainChatStorage.PollDefinition) error
	GetPollDefinition(deviceID, chatJID, pollMessageID string) (*domainChatStorage.PollDefinition, error)
	GetPollDefinitionByIDAndDevice(deviceID, pollMessageID string) (*domainChatStorage.PollDefinition, error)
	AppendPollOption(deviceID, chatJID, pollMessageID string, option domainChatStorage.PollOption) error
}

type webhookPollOptionPayload struct {
	Name string `json:"name"`
	Hash string `json:"hash"`
}

type webhookPollPayload struct {
	Type                  string                     `json:"type"`
	PollID                string                     `json:"poll_id"`
	Question              string                     `json:"question,omitempty"`
	Options               []webhookPollOptionPayload `json:"options,omitempty"`
	SelectableOptionCount uint32                     `json:"selectable_options_count,omitempty"`
	Version               string                     `json:"version,omitempty"`
	SelectedOptions       *[]string                  `json:"selected_options,omitempty"`
	SelectedOptionHashes  *[]string                  `json:"selected_option_hashes,omitempty"`
	ResolutionStatus      string                     `json:"resolution_status,omitempty"`
	AddedOption           *webhookPollOptionPayload  `json:"added_option,omitempty"`
}

func pollOptionHash(name string) string {
	hashes := whatsmeow.HashPollOptions([]string{name})
	if len(hashes) == 0 {
		return ""
	}
	return hex.EncodeToString(hashes[0])
}

func pollOptionsFromProto(options []*waE2E.PollCreationMessage_Option) []domainChatStorage.PollOption {
	result := make([]domainChatStorage.PollOption, 0, len(options))
	for _, option := range options {
		if option == nil {
			continue
		}
		name := option.GetOptionName()
		result = append(result, domainChatStorage.PollOption{Name: name, Hash: pollOptionHash(name)})
	}
	return result
}

func pollDeviceID(ctx context.Context, client *whatsmeow.Client) string {
	if instance, ok := DeviceFromContext(ctx); ok && instance != nil {
		if jid := instance.JID(); jid != "" {
			return jid
		}
		return instance.ID()
	}
	if client != nil && client.Store != nil && client.Store.ID != nil {
		return client.Store.ID.ToNonAD().String()
	}
	return ""
}

func pollChatIDs(ctx context.Context, client *whatsmeow.Client, evt *events.Message, referencedChats ...string) []string {
	if evt == nil {
		return nil
	}
	var result []string
	seen := make(map[string]struct{})
	add := func(jid types.JID) {
		if jid.IsEmpty() {
			return
		}
		for _, candidate := range []string{
			NormalizeJIDFromLID(ctx, jid, client).ToNonAD().String(),
			jid.ToNonAD().String(),
		} {
			if candidate == "" {
				continue
			}
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			result = append(result, candidate)
		}
	}
	add(evt.Info.Chat)
	for _, chat := range referencedChats {
		jid, err := types.ParseJID(chat)
		if err == nil {
			add(jid)
		}
	}
	return result
}

func pollDefinitionFromCreation(deviceID, chatJID, pollID, version string, poll *waE2E.PollCreationMessage, updatedAt ...time.Time) *domainChatStorage.PollDefinition {
	if poll == nil {
		return nil
	}
	definition := &domainChatStorage.PollDefinition{
		DeviceID:              deviceID,
		ChatJID:               chatJID,
		PollMessageID:         pollID,
		Question:              poll.GetName(),
		Options:               pollOptionsFromProto(poll.GetOptions()),
		SelectableOptionCount: poll.GetSelectableOptionsCount(),
		Version:               version,
	}
	if len(updatedAt) > 0 {
		definition.UpdatedAt = updatedAt[0]
	}
	return definition
}

func webhookPollFromDefinition(kind string, definition *domainChatStorage.PollDefinition) *webhookPollPayload {
	payload := &webhookPollPayload{Type: kind}
	if definition == nil {
		return payload
	}
	payload.PollID = definition.PollMessageID
	payload.Question = definition.Question
	payload.SelectableOptionCount = definition.SelectableOptionCount
	payload.Version = definition.Version
	payload.Options = make([]webhookPollOptionPayload, 0, len(definition.Options))
	for _, option := range definition.Options {
		payload.Options = append(payload.Options, webhookPollOptionPayload{Name: option.Name, Hash: option.Hash})
	}
	return payload
}

func loadPollDefinition(store pollDefinitionStore, deviceID, pollID string, chatIDs ...string) *domainChatStorage.PollDefinition {
	if store == nil || deviceID == "" || pollID == "" {
		return nil
	}
	seen := make(map[string]struct{})
	for _, chatID := range chatIDs {
		if chatID == "" {
			continue
		}
		if _, exists := seen[chatID]; exists {
			continue
		}
		seen[chatID] = struct{}{}
		definition, err := store.GetPollDefinition(deviceID, chatID, pollID)
		if err != nil {
			logrus.Warnf("Failed to load poll definition %s for chat %s: %v", pollID, chatID, err)
			continue
		}
		if definition != nil {
			return definition
		}
	}
	definition, err := store.GetPollDefinitionByIDAndDevice(deviceID, pollID)
	if err != nil {
		logrus.Warnf("Failed to resolve poll definition %s by device-scoped message ID: %v", pollID, err)
		return nil
	}
	return definition
}

func resolvePollSelections(definition *domainChatStorage.PollDefinition, selected [][]byte) (names, hashes []string, status string) {
	names = make([]string, 0, len(selected))
	hashes = make([]string, 0, len(selected))
	nameByHash := make(map[string]string)
	if definition != nil {
		for _, option := range definition.Options {
			nameByHash[strings.ToLower(option.Hash)] = option.Name
		}
	}
	unmatched := 0
	for _, selectedHash := range selected {
		hash := hex.EncodeToString(selectedHash)
		hashes = append(hashes, hash)
		if name, ok := nameByHash[hash]; ok {
			names = append(names, name)
		} else {
			unmatched++
		}
	}
	if definition == nil {
		return names, hashes, pollResolutionDefinitionMissing
	}
	if unmatched > 0 {
		return names, hashes, pollResolutionPartiallyResolved
	}
	return names, hashes, pollResolutionResolved
}

func preparePollWebhookPayload(ctx context.Context, client *whatsmeow.Client, store pollDefinitionStore, evt *events.Message) *webhookPollPayload {
	if evt == nil || evt.Message == nil {
		return nil
	}
	// The event handler runs with the context captured at registration; for
	// REST-initiated logins that is the HTTP request context, canceled once
	// the request returns. Detach so LID normalization and message-secret
	// reads during vote/edit decryption survive, keeping device-scoped values.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	deviceID := pollDeviceID(ctx, client)
	chatIDs := pollChatIDs(ctx, client, evt)
	chatJID := ""
	if len(chatIDs) > 0 {
		chatJID = chatIDs[0]
	}
	msg := utils.UnwrapMessage(evt.Message)

	if poll, version := utils.ExtractPollCreationMessage(msg); poll != nil {
		definition := pollDefinitionFromCreation(deviceID, chatJID, evt.Info.ID, version, poll, evt.Info.Timestamp)
		if store != nil && definition.DeviceID != "" && definition.ChatJID != "" {
			if err := store.UpsertPollDefinition(definition); err != nil {
				logrus.Warnf("Failed to persist poll definition %s: %v", evt.Info.ID, err)
			}
		}
		return webhookPollFromDefinition("creation", definition)
	}

	if update := msg.GetPollUpdateMessage(); update != nil {
		pollID := update.GetPollCreationMessageKey().GetID()
		voteChatIDs := pollChatIDs(ctx, client, evt, update.GetPollCreationMessageKey().GetRemoteJID())
		definition := loadPollDefinition(store, deviceID, pollID, voteChatIDs...)
		payload := webhookPollFromDefinition("vote", definition)
		payload.PollID = pollID
		if client == nil {
			payload.ResolutionStatus = pollResolutionDecryptFailed
			return payload
		}
		vote, err := client.DecryptPollVote(ctx, evt)
		if err != nil {
			logrus.Warnf("Failed to decrypt poll vote %s for poll %s: %v", evt.Info.ID, pollID, err)
			payload.ResolutionStatus = pollResolutionDecryptFailed
			return payload
		}
		names, hashes, status := resolvePollSelections(definition, vote.GetSelectedOptions())
		payload.SelectedOptions = &names
		payload.SelectedOptionHashes = &hashes
		payload.ResolutionStatus = status
		return payload
	}

	if add := msg.GetPollAddOptionMessage(); add != nil {
		addChatIDs := pollChatIDs(ctx, client, evt, add.GetPollCreationMessageKey().GetRemoteJID())
		addChatID := chatJID
		if definition := loadPollDefinition(store, deviceID, add.GetPollCreationMessageKey().GetID(), addChatIDs...); definition != nil {
			addChatID = definition.ChatJID
		}
		return preparePollAddOptionPayload(store, deviceID, addChatID, add)
	}

	secret := msg.GetSecretEncryptedMessage()
	if secret == nil || (secret.GetSecretEncType() != waE2E.SecretEncryptedMessage_POLL_EDIT && secret.GetSecretEncType() != waE2E.SecretEncryptedMessage_POLL_ADD_OPTION) {
		return nil
	}
	kind := "edit"
	if secret.GetSecretEncType() == waE2E.SecretEncryptedMessage_POLL_ADD_OPTION {
		kind = "add_option"
	}
	pollID := secret.GetTargetMessageKey().GetID()
	updateChatIDs := pollChatIDs(ctx, client, evt, secret.GetTargetMessageKey().GetRemoteJID())
	degraded := func() *webhookPollPayload {
		payload := webhookPollFromDefinition(kind, loadPollDefinition(store, deviceID, pollID, updateChatIDs...))
		payload.PollID = pollID
		payload.ResolutionStatus = pollResolutionDecryptFailed
		return payload
	}
	if client == nil {
		return degraded()
	}
	decrypted, err := client.DecryptSecretEncryptedMessage(ctx, evt)
	if err != nil {
		logrus.Warnf("Failed to decrypt poll update %s for poll %s: %v", evt.Info.ID, pollID, err)
		return degraded()
	}
	decrypted = utils.UnwrapMessage(decrypted)
	if kind == "add_option" {
		if add := decrypted.GetPollAddOptionMessage(); add != nil {
			addChatID := chatJID
			if definition := loadPollDefinition(store, deviceID, pollID, updateChatIDs...); definition != nil {
				addChatID = definition.ChatJID
			}
			return preparePollAddOptionPayload(store, deviceID, addChatID, add, pollID)
		}
		return degraded()
	}
	poll, version := utils.ExtractPollCreationMessage(decrypted)
	if poll == nil {
		return degraded()
	}
	if existing := loadPollDefinition(store, deviceID, pollID, updateChatIDs...); existing != nil {
		chatJID = existing.ChatJID
	}
	definition := pollDefinitionFromCreation(deviceID, chatJID, pollID, version, poll, evt.Info.Timestamp)
	if store != nil {
		if err := store.UpsertPollDefinition(definition); err != nil {
			logrus.Warnf("Failed to persist edited poll definition %s: %v", pollID, err)
		}
	}
	return webhookPollFromDefinition("edit", definition)
}

func preparePollAddOptionPayload(store pollDefinitionStore, deviceID, chatJID string, add *waE2E.PollAddOptionMessage, fallbackPollID ...string) *webhookPollPayload {
	if add == nil {
		return nil
	}
	pollID := add.GetPollCreationMessageKey().GetID()
	if pollID == "" && len(fallbackPollID) > 0 {
		pollID = fallbackPollID[0]
	}
	option := domainChatStorage.PollOption{Name: add.GetAddOption().GetOptionName()}
	option.Hash = pollOptionHash(option.Name)
	if store != nil && option.Name != "" {
		if err := store.AppendPollOption(deviceID, chatJID, pollID, option); err != nil {
			logrus.Warnf("Failed to append option to poll %s: %v", pollID, err)
		}
	}
	payload := webhookPollFromDefinition("add_option", loadPollDefinition(store, deviceID, pollID, chatJID))
	payload.PollID = pollID
	payload.AddedOption = &webhookPollOptionPayload{Name: option.Name, Hash: option.Hash}
	if payload.Question == "" {
		payload.ResolutionStatus = pollResolutionDefinitionMissing
	}
	return payload
}

func pollWebhookBody(payload *webhookPollPayload) string {
	if payload == nil {
		return ""
	}
	switch payload.Type {
	case "creation":
		if payload.Question != "" {
			return "Poll: " + payload.Question
		}
		return "Poll"
	case "edit":
		if payload.Question != "" {
			return "Poll edited: " + payload.Question
		}
		return "Poll edited"
	case "add_option":
		if payload.AddedOption != nil && payload.AddedOption.Name != "" {
			return "Poll option added: " + payload.AddedOption.Name
		}
		return "Poll option added"
	case "vote":
		if payload.ResolutionStatus == pollResolutionResolved && payload.SelectedOptions != nil {
			if len(*payload.SelectedOptions) == 0 {
				return "Poll vote cleared"
			}
			return "Poll vote: " + strings.Join(*payload.SelectedOptions, ", ")
		}
		if payload.ResolutionStatus == pollResolutionPartiallyResolved && payload.SelectedOptions != nil && len(*payload.SelectedOptions) > 0 {
			return fmt.Sprintf("Poll vote: %s (partially resolved)", strings.Join(*payload.SelectedOptions, ", "))
		}
		label := strings.ReplaceAll(payload.ResolutionStatus, "_", " ")
		if payload.PollID != "" {
			return fmt.Sprintf("Poll vote: %s (%s)", payload.PollID, label)
		}
		return "Poll vote (" + label + ")"
	default:
		return "Poll"
	}
}
