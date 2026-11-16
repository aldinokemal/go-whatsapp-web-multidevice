package whatsapp

import (
	"context"
	"fmt"
	"os"

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
func PurgeLocalChat(repo domainChatStorage.IChatStorageRepository, deviceID string, jids []string, deleteMedia bool) error {
	if repo == nil || deviceID == "" || len(jids) == 0 {
		return fmt.Errorf("chat storage, device ID and chat JID are required")
	}
	if err := repo.DeleteChatByDevice(deviceID, jids[0], jids[1:]...); err != nil {
		return err
	}
	if !deleteMedia {
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
	jids, err := ChatDeleteJIDs(ctx, client, evt.JID)
	if err == nil {
		err = PurgeLocalChat(repo, deviceID, jids, evt.DeleteMedia)
	}
	if err != nil {
		log.Errorf("Failed to purge deleted chat %s for device %s: %v", evt.JID, deviceID, err)
	}
}
