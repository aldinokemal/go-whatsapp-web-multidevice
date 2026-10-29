package whatsapp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"testing"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	projectSQLite "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/proto/waCommon"
	"go.mau.fi/whatsmeow/proto/waE2E"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
	"go.mau.fi/whatsmeow/util/gcmutil"
	"go.mau.fi/whatsmeow/util/hkdfutil"
	waLog "go.mau.fi/whatsmeow/util/log"
	"google.golang.org/protobuf/proto"
)

type memoryPollStore struct {
	definitions map[string]*domainChatStorage.PollDefinition
}

func newMemoryPollStore() *memoryPollStore {
	return &memoryPollStore{definitions: make(map[string]*domainChatStorage.PollDefinition)}
}

func pollStoreKey(deviceID, chatJID, pollID string) string {
	return deviceID + "\x00" + chatJID + "\x00" + pollID
}

func (s *memoryPollStore) UpsertPollDefinition(definition *domainChatStorage.PollDefinition) error {
	copyDefinition := *definition
	copyDefinition.Options = append([]domainChatStorage.PollOption(nil), definition.Options...)
	s.definitions[pollStoreKey(definition.DeviceID, definition.ChatJID, definition.PollMessageID)] = &copyDefinition
	return nil
}

func (s *memoryPollStore) GetPollDefinition(deviceID, chatJID, pollMessageID string) (*domainChatStorage.PollDefinition, error) {
	definition := s.definitions[pollStoreKey(deviceID, chatJID, pollMessageID)]
	if definition == nil {
		return nil, nil
	}
	copyDefinition := *definition
	copyDefinition.Options = append([]domainChatStorage.PollOption(nil), definition.Options...)
	return &copyDefinition, nil
}

func (s *memoryPollStore) GetPollDefinitionByIDAndDevice(deviceID, pollMessageID string) (*domainChatStorage.PollDefinition, error) {
	var match *domainChatStorage.PollDefinition
	for _, definition := range s.definitions {
		if definition.DeviceID != deviceID || definition.PollMessageID != pollMessageID {
			continue
		}
		if match != nil {
			return nil, fmt.Errorf("poll definition %s is ambiguous", pollMessageID)
		}
		copyDefinition := *definition
		copyDefinition.Options = append([]domainChatStorage.PollOption(nil), definition.Options...)
		match = &copyDefinition
	}
	return match, nil
}

func (s *memoryPollStore) AppendPollOption(deviceID, chatJID, pollMessageID string, option domainChatStorage.PollOption) error {
	definition := s.definitions[pollStoreKey(deviceID, chatJID, pollMessageID)]
	if definition == nil {
		return fmt.Errorf("poll definition %s not found", pollMessageID)
	}
	for _, existing := range definition.Options {
		if existing.Hash == option.Hash {
			return nil
		}
	}
	definition.Options = append(definition.Options, option)
	return nil
}

func TestPreparePollWebhookPayloadStoresCreation(t *testing.T) {
	store := newMemoryPollStore()
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance("device-a", nil, nil))
	evt := &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: types.NewJID("120363000000", types.GroupServer)},
			ID:            "POLL-1",
		},
		Message: &waE2E.Message{PollCreationMessageV3: &waE2E.PollCreationMessage{
			Name:                   proto.String("Lunch?"),
			SelectableOptionsCount: proto.Uint32(1),
			Options: []*waE2E.PollCreationMessage_Option{
				{OptionName: proto.String("Pizza")},
				{OptionName: proto.String("Sushi")},
			},
		}},
	}

	payload := preparePollWebhookPayload(ctx, nil, store, evt)
	if payload == nil || payload.Type != "creation" || payload.PollID != "POLL-1" || payload.Question != "Lunch?" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Options) != 2 || payload.Options[1].Name != "Sushi" || payload.Options[1].Hash == "" {
		t.Fatalf("unexpected options: %+v", payload.Options)
	}
	stored, err := store.GetPollDefinition("device-a", evt.Info.Chat.String(), "POLL-1")
	if err != nil || stored == nil || stored.Version != "v3" || len(stored.Options) != 2 {
		t.Fatalf("stored definition=%+v err=%v", stored, err)
	}
}

func TestResolvePollSelectionsStatuses(t *testing.T) {
	definition := &domainChatStorage.PollDefinition{Options: []domainChatStorage.PollOption{
		{Name: "Pizza", Hash: pollOptionHash("Pizza")},
		{Name: "Sushi", Hash: pollOptionHash("Sushi")},
	}}

	t.Run("resolved", func(t *testing.T) {
		names, hashes, status := resolvePollSelections(definition, [][]byte{
			whatsmeow.HashPollOptions([]string{"Sushi"})[0],
		})
		if status != "resolved" || len(names) != 1 || names[0] != "Sushi" || len(hashes) != 1 {
			t.Fatalf("names=%v hashes=%v status=%s", names, hashes, status)
		}
	})

	t.Run("cleared", func(t *testing.T) {
		names, hashes, status := resolvePollSelections(definition, nil)
		if status != "resolved" || names == nil || hashes == nil || len(names) != 0 || len(hashes) != 0 {
			t.Fatalf("names=%v hashes=%v status=%s", names, hashes, status)
		}
	})

	t.Run("partially resolved", func(t *testing.T) {
		names, hashes, status := resolvePollSelections(definition, [][]byte{
			whatsmeow.HashPollOptions([]string{"Pizza"})[0],
			bytes.Repeat([]byte{0xff}, 32),
		})
		if status != "partially_resolved" || len(names) != 1 || len(hashes) != 2 {
			t.Fatalf("names=%v hashes=%v status=%s", names, hashes, status)
		}
	})

	t.Run("definition missing", func(t *testing.T) {
		names, hashes, status := resolvePollSelections(nil, [][]byte{
			whatsmeow.HashPollOptions([]string{"Pizza"})[0],
		})
		if status != "definition_missing" || names == nil || len(names) != 0 || len(hashes) != 1 {
			t.Fatalf("names=%v hashes=%v status=%s", names, hashes, status)
		}
	})
}

func TestPreparePollWebhookPayloadDecryptsRealVote(t *testing.T) {
	ctx := context.Background()
	container, err := sqlstore.New(ctx, projectSQLite.DriverName, projectSQLite.FormatChatStorageURI("file:poll-event-test?mode=memory&cache=shared", false, true), nil)
	if err != nil {
		t.Fatalf("sqlstore.New: %v", err)
	}
	defer container.Close()

	device := container.NewDevice()
	voter := types.NewJID("628222", types.DefaultUserServer)
	device.ID = &voter
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{0},
		AccountSignatureKey: make([]byte, 32),
		AccountSignature:    make([]byte, 64),
		DeviceSignature:     make([]byte, 64),
	}
	if err := device.Save(ctx); err != nil {
		t.Fatalf("device.Save: %v", err)
	}
	client := whatsmeow.NewClient(device, nil)
	chat := types.NewJID("120363000000", types.GroupServer)
	creator := types.NewJID("628111", types.DefaultUserServer)
	pollID := types.MessageID("POLL-CRYPTO-1")
	if err := device.MsgSecrets.PutMessageSecret(ctx, chat, creator, pollID, bytes.Repeat([]byte{0x42}, 32)); err != nil {
		t.Fatalf("PutMessageSecret: %v", err)
	}

	voteMessage, err := client.BuildPollVote(ctx, &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: creator, IsGroup: true},
		ID:            pollID,
	}, []string{"Sushi"})
	if err != nil {
		t.Fatalf("BuildPollVote: %v", err)
	}
	store := newMemoryPollStore()
	if err := store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: voter.String(), ChatJID: chat.String(), PollMessageID: string(pollID), Question: "Lunch?",
		Options: []domainChatStorage.PollOption{
			{Name: "Pizza", Hash: pollOptionHash("Pizza")},
			{Name: "Sushi", Hash: pollOptionHash("Sushi")},
		},
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}

	payload := preparePollWebhookPayload(ctx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: voter, IsGroup: true, IsFromMe: true},
			ID:            "VOTE-1",
		},
		Message: voteMessage,
	})
	if payload == nil || payload.ResolutionStatus != "resolved" || payload.SelectedOptions == nil || len(*payload.SelectedOptions) != 1 || (*payload.SelectedOptions)[0] != "Sushi" {
		t.Fatalf("unexpected decrypted payload: %+v", payload)
	}

	badContainer, err := sqlstore.New(ctx, projectSQLite.DriverName, projectSQLite.FormatChatStorageURI("file:poll-event-bad-secret-test?mode=memory&cache=shared", false, true), nil)
	if err != nil {
		t.Fatalf("bad sqlstore.New: %v", err)
	}
	defer badContainer.Close()
	badDevice := badContainer.NewDevice()
	badDevice.ID = &voter
	badDevice.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{0},
		AccountSignatureKey: make([]byte, 32),
		AccountSignature:    make([]byte, 64),
		DeviceSignature:     make([]byte, 64),
	}
	if err := badDevice.Save(ctx); err != nil {
		t.Fatalf("bad device.Save: %v", err)
	}
	if err := badDevice.MsgSecrets.PutMessageSecret(ctx, chat, creator, pollID, bytes.Repeat([]byte{0x99}, 32)); err != nil {
		t.Fatalf("store mismatched message secret: %v", err)
	}
	failed := preparePollWebhookPayload(ctx, whatsmeow.NewClient(badDevice, nil), store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: voter, IsGroup: true, IsFromMe: true},
			ID:            "VOTE-FAILED-1",
		},
		Message: voteMessage,
	})
	if failed == nil || failed.ResolutionStatus != pollResolutionDecryptFailed || failed.Question != "Lunch?" || failed.SelectedOptions != nil || failed.SelectedOptionHashes != nil {
		t.Fatalf("unexpected authentication-failure payload: %+v", failed)
	}
}

// newPollCryptoClient builds a whatsmeow client over an isolated in-memory
// device store so tests can exercise real message-secret encryption.
func newPollCryptoClient(t *testing.T, dbName string, deviceJID types.JID) *whatsmeow.Client {
	t.Helper()
	ctx := context.Background()
	container, err := sqlstore.New(ctx, projectSQLite.DriverName, projectSQLite.FormatChatStorageURI("file:"+dbName+"?mode=memory&cache=shared", false, true), nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = container.Close() })
	device := container.NewDevice()
	device.ID = &deviceJID
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{0},
		AccountSignatureKey: make([]byte, 32),
		AccountSignature:    make([]byte, 64),
		DeviceSignature:     make([]byte, 64),
	}
	require.NoError(t, device.Save(ctx))
	return whatsmeow.NewClient(device, nil)
}

func TestPreparePollWebhookPayloadDecryptsVoteAfterHandlerContextCanceled(t *testing.T) {
	setupCtx := context.Background()
	voter := types.NewJID("628222", types.DefaultUserServer)
	client := newPollCryptoClient(t, "poll-event-canceled-ctx-test", voter)
	chat := types.NewJID("120363000000", types.GroupServer)
	creator := types.NewJID("628111", types.DefaultUserServer)
	pollID := types.MessageID("POLL-CANCELED-CTX-1")
	require.NoError(t, client.Store.MsgSecrets.PutMessageSecret(setupCtx, chat, creator, pollID, bytes.Repeat([]byte{0x42}, 32)))

	voteMessage, err := client.BuildPollVote(setupCtx, &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: creator, IsGroup: true},
		ID:            pollID,
	}, []string{"Sushi"})
	require.NoError(t, err)
	store := newMemoryPollStore()
	require.NoError(t, store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: voter.String(), ChatJID: chat.String(), PollMessageID: string(pollID), Question: "Lunch?",
		Options: []domainChatStorage.PollOption{
			{Name: "Pizza", Hash: pollOptionHash("Pizza")},
			{Name: "Sushi", Hash: pollOptionHash("Sushi")},
		},
	}))

	// The whatsmeow event handler runs with the context captured at
	// registration; for REST-initiated logins that is the HTTP request
	// context, already canceled by the time later poll events arrive.
	handlerCtx, cancelHandlerCtx := context.WithCancel(context.Background())
	cancelHandlerCtx()

	payload := preparePollWebhookPayload(handlerCtx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: voter, IsGroup: true, IsFromMe: true},
			ID:            "VOTE-CANCELED-CTX-1",
		},
		Message: voteMessage,
	})
	require.NotNil(t, payload)
	assert.Equal(t, pollResolutionResolved, payload.ResolutionStatus)
	require.NotNil(t, payload.SelectedOptions)
	assert.Equal(t, []string{"Sushi"}, *payload.SelectedOptions)
}

func TestPreparePollWebhookPayloadDegradesAddOptionWithoutInnerMessage(t *testing.T) {
	ctx := context.Background()
	voter := types.NewJID("628222", types.DefaultUserServer)
	client := newPollCryptoClient(t, "poll-event-add-option-empty-test", voter)
	chat := types.NewJID("120363000000", types.GroupServer)
	pollID := "POLL-ADD-EMPTY-1"
	secret := bytes.Repeat([]byte{0x37}, 32)
	require.NoError(t, client.Store.MsgSecrets.PutMessageSecret(ctx, chat, voter, pollID, secret))

	store := newMemoryPollStore()
	require.NoError(t, store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: voter.String(), ChatJID: chat.String(), PollMessageID: pollID, Question: "Lunch?",
		Options: []domainChatStorage.PollOption{{Name: "Pizza", Hash: pollOptionHash("Pizza")}},
	}))

	// Encrypt an inner message that carries no PollAddOptionMessage, using the
	// same "Poll Edit" secret derivation whatsmeow applies to POLL_ADD_OPTION.
	plaintext, err := proto.Marshal(&waE2E.Message{})
	require.NoError(t, err)
	useCaseSecret := pollID + voter.ToNonAD().String() + voter.ToNonAD().String() + "Poll Edit"
	secretKey := hkdfutil.SHA256(secret, nil, []byte(useCaseSecret), 32)
	iv := bytes.Repeat([]byte{0x11}, 12)
	ciphertext, err := gcmutil.Encrypt(secretKey, iv, plaintext, nil)
	require.NoError(t, err)

	payload := preparePollWebhookPayload(ctx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: voter, IsGroup: true, IsFromMe: true},
			ID:            "ADD-EMPTY-1",
		},
		Message: &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType:    waE2E.SecretEncryptedMessage_POLL_ADD_OPTION.Enum(),
			TargetMessageKey: &waCommon.MessageKey{RemoteJID: proto.String(chat.String()), FromMe: proto.Bool(true), ID: proto.String(pollID)},
			EncIV:            iv,
			EncPayload:       ciphertext,
		}},
	})
	require.NotNil(t, payload)
	assert.Equal(t, "add_option", payload.Type)
	assert.Equal(t, pollID, payload.PollID)
	assert.Equal(t, "Lunch?", payload.Question)
	assert.Equal(t, pollResolutionDecryptFailed, payload.ResolutionStatus)
	assert.Nil(t, payload.AddedOption)
}

func TestPreparePollWebhookPayloadDecryptsLIDGroupVote(t *testing.T) {
	ctx := context.Background()
	container, err := sqlstore.New(ctx, projectSQLite.DriverName, projectSQLite.FormatChatStorageURI("file:poll-event-lid-test?mode=memory&cache=shared", false, true), nil)
	if err != nil {
		t.Fatalf("sqlstore.New: %v", err)
	}
	defer container.Close()

	device := container.NewDevice()
	voterPN := types.NewJID("628222", types.DefaultUserServer)
	voterLID := types.NewJID("222000000000", types.HiddenUserServer)
	device.ID = &voterPN
	device.LID = voterLID
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte{0},
		AccountSignatureKey: make([]byte, 32),
		AccountSignature:    make([]byte, 64),
		DeviceSignature:     make([]byte, 64),
	}
	if err := device.Save(ctx); err != nil {
		t.Fatalf("device.Save: %v", err)
	}
	client := whatsmeow.NewClient(device, nil)
	chat := types.NewJID("120363000001", types.GroupServer)
	creatorLID := types.NewJID("111000000000", types.HiddenUserServer)
	pollID := types.MessageID("POLL-LID-1")
	if err := device.MsgSecrets.PutMessageSecret(ctx, chat, creatorLID, pollID, bytes.Repeat([]byte{0x24}, 32)); err != nil {
		t.Fatalf("PutMessageSecret: %v", err)
	}
	voteMessage, err := client.BuildPollVote(ctx, &types.MessageInfo{
		MessageSource: types.MessageSource{Chat: chat, Sender: creatorLID, IsGroup: true, AddressingMode: types.AddressingModeLID},
		ID:            pollID,
	}, []string{"Yes"})
	if err != nil {
		t.Fatalf("BuildPollVote: %v", err)
	}
	store := newMemoryPollStore()
	if err := store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: voterPN.String(), ChatJID: chat.String(), PollMessageID: string(pollID), Question: "LID poll",
		Options: []domainChatStorage.PollOption{{Name: "Yes", Hash: pollOptionHash("Yes")}},
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	payload := preparePollWebhookPayload(ctx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{Chat: chat, Sender: voterLID, IsGroup: true, IsFromMe: true, AddressingMode: types.AddressingModeLID},
			ID:            "VOTE-LID-1",
		},
		Message: voteMessage,
	})
	if payload == nil || payload.ResolutionStatus != pollResolutionResolved || payload.SelectedOptions == nil || len(*payload.SelectedOptions) != 1 || (*payload.SelectedOptions)[0] != "Yes" {
		t.Fatalf("unexpected LID payload: %+v", payload)
	}
}

func TestPreparePollWebhookPayloadFindsDefinitionStoredBeforeLIDMapping(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	ctx := context.Background()
	voterPN := types.NewJID("628222", types.DefaultUserServer)
	voterLID := types.NewJID("222000000000", types.HiddenUserServer)
	chatPN := types.NewJID("628111", types.DefaultUserServer)
	chatLID := types.NewJID("111000000000", types.HiddenUserServer)
	client := newPollCryptoClient(t, "poll-event-late-lid-map-test", voterPN)
	client.Store.LID = voterLID
	pollID := types.MessageID("POLL-LATE-LID-1")
	require.NoError(t, client.Store.MsgSecrets.PutMessageSecret(ctx, chatLID, chatLID, pollID, bytes.Repeat([]byte{0x63}, 32)))

	voteMessage, err := client.BuildPollVote(ctx, &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:           chatLID,
			Sender:         chatLID,
			AddressingMode: types.AddressingModeLID,
		},
		ID: pollID,
	}, []string{"Yes"})
	require.NoError(t, err)

	store := newMemoryPollStore()
	require.NoError(t, store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID:      voterPN.String(),
		ChatJID:       chatLID.String(),
		PollMessageID: string(pollID),
		Question:      "Mapped later?",
		Options:       []domainChatStorage.PollOption{{Name: "Yes", Hash: pollOptionHash("Yes")}},
	}))

	// The poll was stored while the chat only had a LID. The mapping becomes
	// available before the vote, so normalization now produces the PN alias.
	require.NoError(t, client.Store.LIDs.PutLIDMapping(ctx, chatLID, chatPN))
	payload := preparePollWebhookPayload(ctx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:           chatLID,
				Sender:         voterLID,
				IsFromMe:       true,
				AddressingMode: types.AddressingModeLID,
			},
			ID: "VOTE-LATE-LID-1",
		},
		Message: voteMessage,
	})
	require.NotNil(t, payload)
	assert.Equal(t, pollResolutionResolved, payload.ResolutionStatus)
	require.NotNil(t, payload.SelectedOptions)
	assert.Equal(t, []string{"Yes"}, *payload.SelectedOptions)
}

func TestPreparePollWebhookPayloadFindsPNDefinitionForUnmappedLIDVote(t *testing.T) {
	originalLog := log
	log = waLog.Noop
	defer func() { log = originalLog }()

	ctx := context.Background()
	voterPN := types.NewJID("628222", types.DefaultUserServer)
	voterLID := types.NewJID("222000000000", types.HiddenUserServer)
	chatPN := types.NewJID("628111", types.DefaultUserServer)
	chatLID := types.NewJID("111000000000", types.HiddenUserServer)
	client := newPollCryptoClient(t, "poll-event-unmapped-lid-test", voterPN)
	client.Store.LID = voterLID
	pollID := types.MessageID("POLL-UNMAPPED-LID-1")
	require.NoError(t, client.Store.MsgSecrets.PutMessageSecret(ctx, chatLID, chatLID, pollID, bytes.Repeat([]byte{0x73}, 32)))

	voteMessage, err := client.BuildPollVote(ctx, &types.MessageInfo{
		MessageSource: types.MessageSource{
			Chat:           chatLID,
			Sender:         chatLID,
			AddressingMode: types.AddressingModeLID,
		},
		ID: pollID,
	}, []string{"Yes"})
	require.NoError(t, err)

	store := newMemoryPollStore()
	require.NoError(t, store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID:      voterPN.String(),
		ChatJID:       chatPN.String(),
		PollMessageID: string(pollID),
		Question:      "PN creation",
		Options:       []domainChatStorage.PollOption{{Name: "Yes", Hash: pollOptionHash("Yes")}},
	}))

	// No LID mapping is available. The poll message ID is the only stable chat-
	// independent identity shared by the PN creation and the LID-only vote.
	payload := preparePollWebhookPayload(ctx, client, store, &events.Message{
		Info: types.MessageInfo{
			MessageSource: types.MessageSource{
				Chat:           chatLID,
				Sender:         voterLID,
				IsFromMe:       true,
				AddressingMode: types.AddressingModeLID,
			},
			ID: "VOTE-UNMAPPED-LID-1",
		},
		Message: voteMessage,
	})
	require.NotNil(t, payload)
	assert.Equal(t, pollResolutionResolved, payload.ResolutionStatus)
	require.NotNil(t, payload.SelectedOptions)
	assert.Equal(t, []string{"Yes"}, *payload.SelectedOptions)
}

func TestPreparePollWebhookPayloadAppliesAddOptionIdempotently(t *testing.T) {
	store := newMemoryPollStore()
	deviceID := "device-a"
	chat := types.NewJID("120363000000", types.GroupServer)
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	if err := store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chat.String(), PollMessageID: "POLL-ADD-1", Question: "Lunch?",
		Options: []domainChatStorage.PollOption{{Name: "Pizza", Hash: pollOptionHash("Pizza")}},
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	evt := &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat}, ID: "ADD-1"},
		Message: &waE2E.Message{PollAddOptionMessage: &waE2E.PollAddOptionMessage{
			PollCreationMessageKey: &waCommon.MessageKey{ID: proto.String("POLL-ADD-1")},
			AddOption:              &waE2E.PollCreationMessage_Option{OptionName: proto.String("Sushi")},
		}},
	}

	for range 2 {
		payload := preparePollWebhookPayload(ctx, nil, store, evt)
		if payload == nil || payload.Type != "add_option" || payload.AddedOption == nil || payload.AddedOption.Name != "Sushi" {
			t.Fatalf("unexpected add-option payload: %+v", payload)
		}
	}
	definition, err := store.GetPollDefinition(deviceID, chat.String(), "POLL-ADD-1")
	if err != nil || definition == nil || len(definition.Options) != 2 || definition.Options[1].Name != "Sushi" {
		t.Fatalf("definition=%+v err=%v", definition, err)
	}
}

func TestPreparePollWebhookPayloadDegradesEncryptedPollUpdateWithoutClient(t *testing.T) {
	store := newMemoryPollStore()
	deviceID := "device-a"
	chat := types.NewJID("120363000000", types.GroupServer)
	ctx := ContextWithDevice(context.Background(), NewDeviceInstance(deviceID, nil, nil))
	if err := store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chat.String(), PollMessageID: "POLL-EDIT-1", Question: "Old question",
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	payload := preparePollWebhookPayload(ctx, nil, store, &events.Message{
		Info: types.MessageInfo{MessageSource: types.MessageSource{Chat: chat}, ID: "EDIT-1"},
		Message: &waE2E.Message{SecretEncryptedMessage: &waE2E.SecretEncryptedMessage{
			SecretEncType:    waE2E.SecretEncryptedMessage_POLL_EDIT.Enum(),
			TargetMessageKey: &waCommon.MessageKey{ID: proto.String("POLL-EDIT-1")},
		}},
	})
	if payload == nil || payload.Type != "edit" || payload.PollID != "POLL-EDIT-1" || payload.Question != "Old question" || payload.ResolutionStatus != pollResolutionDecryptFailed {
		t.Fatalf("unexpected degraded payload: %+v", payload)
	}
}

func TestWebhookPollPayloadKeepsClearedSelectionArrays(t *testing.T) {
	names := make([]string, 0)
	hashes := make([]string, 0)
	encoded, err := json.Marshal(&webhookPollPayload{
		Type: "vote", PollID: "POLL-1", SelectedOptions: &names, SelectedOptionHashes: &hashes,
		ResolutionStatus: pollResolutionResolved,
	})
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if selected, ok := decoded["selected_options"].([]any); !ok || len(selected) != 0 {
		t.Fatalf("selected_options missing or non-empty: %#v", decoded["selected_options"])
	}
	if selected, ok := decoded["selected_option_hashes"].([]any); !ok || len(selected) != 0 {
		t.Fatalf("selected_option_hashes missing or non-empty: %#v", decoded["selected_option_hashes"])
	}
}

func TestPreparePollAddOptionPayloadUsesEncryptedEnvelopePollID(t *testing.T) {
	store := newMemoryPollStore()
	deviceID := "device-a"
	chatJID := "120363000000@g.us"
	if err := store.UpsertPollDefinition(&domainChatStorage.PollDefinition{
		DeviceID: deviceID, ChatJID: chatJID, PollMessageID: "POLL-FALLBACK-1", Question: "Q",
	}); err != nil {
		t.Fatalf("UpsertPollDefinition: %v", err)
	}
	payload := preparePollAddOptionPayload(store, deviceID, chatJID, &waE2E.PollAddOptionMessage{
		AddOption: &waE2E.PollCreationMessage_Option{OptionName: proto.String("New")},
	}, "POLL-FALLBACK-1")
	if payload == nil || payload.PollID != "POLL-FALLBACK-1" || payload.Question != "Q" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
