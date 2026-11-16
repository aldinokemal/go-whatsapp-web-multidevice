package whatsapp

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"go.mau.fi/whatsmeow"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/types/events"
)

// ChatDeleteJIDs resolves both stored forms without changing the durable LID map.
func ChatDeleteJIDs(ctx context.Context, client *whatsmeow.Client, jid types.JID) ([]string, error) {
	jid = jid.ToNonAD()
	jids := []string{jid.String()}
	if client == nil || client.Store == nil || client.Store.LIDs == nil {
		return jids, nil
	}
	var alias types.JID
	var err error
	switch jid.Server {
	case types.HiddenUserServer:
		alias, err = client.Store.LIDs.GetPNForLID(ctx, jid)
	case types.DefaultUserServer:
		alias, err = client.Store.LIDs.GetLIDForPN(ctx, jid)
	}
	if err != nil {
		return nil, fmt.Errorf("resolve chat alias: %w", err)
	}
	if !alias.IsEmpty() && alias.ToNonAD().String() != jid.String() {
		jids = append(jids, alias.ToNonAD().String())
	}
	return jids, nil
}

// PurgeLocalChat is shared by API deletes and deletions received from WhatsApp.
func PurgeLocalChat(repo domainChatStorage.IChatStorageRepository, deviceID string, jids []string, deleteMedia bool, bound *time.Time) error {
	if repo == nil || deviceID == "" || len(jids) == 0 {
		return fmt.Errorf("chat storage, device ID and chat JID are required")
	}
	fullyRemoved := true
	var err error
	if bound == nil {
		err = repo.DeleteChatByDevice(deviceID, jids[0], jids[1:]...)
	} else {
		fullyRemoved, err = repo.DeleteChatByDeviceBefore(deviceID, *bound, jids...)
	}
	if err != nil {
		return err
	}
	if !deleteMedia || !fullyRemoved {
		return nil
	}
	root, err := os.OpenRoot(config.PathMedia)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer root.Close()
	for _, jid := range jids {
		directory := utils.ExtractPhoneNumber(jid)
		if directory == "" {
			continue
		}
		// The legacy cache is shared: keep directories still used by another chat/device.
		referenced, err := repo.IsChatMediaDirectoryReferenced(directory)
		if err != nil {
			return err
		}
		if referenced {
			continue
		}
		if err := root.RemoveAll(directory); err != nil {
			return fmt.Errorf("remove chat media: %w", err)
		}
	}
	// Flat webhook downloads have no stored chat owner and cannot be removed safely.
	return nil
}

func handleDeleteChat(ctx context.Context, evt *events.DeleteChat, repo domainChatStorage.IChatStorageRepository, deviceID string, client *whatsmeow.Client) {
	bound := evt.Timestamp
	if timestamp := evt.Action.GetMessageRange().GetLastMessageTimestamp(); timestamp > 0 {
		bound = time.Unix(timestamp, 0)
	}
	if bound.IsZero() {
		log.Warnf("Skipping deleted chat %s without a message range or event timestamp", evt.JID)
		return
	}
	jids, err := ChatDeleteJIDs(ctx, client, evt.JID)
	if err == nil {
		err = PurgeLocalChat(repo, deviceID, jids, evt.DeleteMedia, &bound)
	}
	if err != nil {
		log.Errorf("Failed to purge deleted chat %s for device %s: %v", evt.JID, deviceID, err)
	}
}
