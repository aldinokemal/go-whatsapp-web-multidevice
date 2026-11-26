package usecase

import (
	"context"
	"fmt"
	"time"

	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	domainLabel "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/label"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
	"go.mau.fi/whatsmeow/appstate"
	"go.mau.fi/whatsmeow/proto/waSyncAction"
	"google.golang.org/protobuf/proto"
)

type serviceLabel struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewLabelService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainLabel.ILabelUsecase {
	return &serviceLabel{chatStorageRepo: chatStorageRepo}
}

func (service *serviceLabel) ListLabels(ctx context.Context) (response domainLabel.ListLabelsResponse, err error) {
	deviceID := storageDeviceID(ctx)
	if deviceID == "" {
		return response, fmt.Errorf("device identification required")
	}
	return service.storedLabels(deviceID)
}

func (service *serviceLabel) ResyncLabels(ctx context.Context) (response domainLabel.ListLabelsResponse, err error) {
	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	deviceID := storageDeviceID(ctx)
	if deviceID == "" {
		return response, fmt.Errorf("device identification required")
	}

	labels, chats, err := whatsapp.FetchLabelState(ctx, client, deviceID)
	if err != nil {
		return response, err
	}
	if err = service.chatStorageRepo.ReplaceLabels(deviceID, labels, chats); err != nil {
		return response, fmt.Errorf("failed to store labels: %w", err)
	}
	logrus.WithFields(logrus.Fields{"labels": len(labels), "label_chats": len(chats)}).Info("Labels resynced")

	return service.storedLabels(deviceID)
}

// storedLabels lists the labels of a device that are not deleted, each with
// the chats it holds.
func (service *serviceLabel) storedLabels(deviceID string) (response domainLabel.ListLabelsResponse, err error) {
	labels, err := service.chatStorageRepo.GetLabels(deviceID)
	if err != nil {
		return response, err
	}
	links, err := service.chatStorageRepo.GetLabelChats(deviceID)
	if err != nil {
		return response, err
	}

	chatsByLabel := map[string][]string{}
	for _, link := range links {
		if link != nil {
			chatsByLabel[link.LabelID] = append(chatsByLabel[link.LabelID], link.ChatJID)
		}
	}

	response.Data = make([]domainLabel.LabelInfo, 0, len(labels))
	for _, label := range labels {
		if label == nil || label.Deleted {
			continue
		}
		chats := chatsByLabel[label.ID]
		if chats == nil {
			chats = []string{}
		}
		response.Data = append(response.Data, domainLabel.LabelInfo{
			ID:         label.ID,
			Name:       label.Name,
			Color:      label.Color,
			Type:       label.Type,
			OrderIndex: label.OrderIndex,
			Active:     label.Active,
			Chats:      chats,
			UpdatedAt:  formatTimestamp(label.UpdatedAt),
		})
	}
	return response, nil
}

func (service *serviceLabel) EditLabel(ctx context.Context, request domainLabel.EditLabelRequest) (response domainLabel.EditLabelResponse, err error) {
	if err = validations.ValidateEditLabel(ctx, &request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	deviceID := storageDeviceID(ctx)
	var stored *domainChatStorage.Label
	if deviceID != "" {
		if stored, err = service.chatStorageRepo.GetLabel(deviceID, request.LabelID); err != nil {
			return response, fmt.Errorf("failed to read label %s: %w", request.LabelID, err)
		}
	}

	patchInfo, label := buildLabelEditPatch(request, stored)
	if err = client.SendAppState(ctx, patchInfo); err != nil {
		logrus.WithError(err).WithField("label_id", request.LabelID).Error("Failed to send label edit app state")
		return response, err
	}

	// The echo of the patch updates storage too; write now so a list right
	// after this call already shows the change.
	if deviceID != "" {
		label.DeviceID = deviceID
		label.UpdatedAt = time.Now().UTC()
		if storeErr := service.chatStorageRepo.UpsertLabel(label); storeErr != nil {
			logrus.WithError(storeErr).WithField("label_id", label.ID).Warn("Failed to store edited label")
		}
	}

	response.Status = "success"
	response.LabelID = label.ID
	response.Name = label.Name
	response.Color = label.Color
	response.Type = label.Type
	response.OrderIndex = label.OrderIndex
	response.Deleted = label.Deleted
	switch {
	case label.Deleted:
		response.Message = "Label deleted successfully"
	case stored == nil:
		response.Message = "Label saved successfully"
	default:
		response.Message = "Label updated successfully"
	}
	return response, nil
}

// buildLabelEditPatch builds the label_edit patch on top of whatsmeow's
// BuildLabelEdit. Fields the request leaves out keep their stored values, so a
// rename does not drop the color, the position or the list type. A typed label
// (a chat list of a personal account) also carries is_active, as the phone
// sends it.
func buildLabelEditPatch(request domainLabel.EditLabelRequest, stored *domainChatStorage.Label) (appstate.PatchInfo, *domainChatStorage.Label) {
	label := &domainChatStorage.Label{ID: request.LabelID, Name: request.Name, Type: request.Type, Active: true, Deleted: request.Deleted}
	if stored != nil {
		label.Color = stored.Color
		label.OrderIndex = stored.OrderIndex
		if label.Type == "" {
			label.Type = stored.Type
		}
		if label.Name == "" {
			label.Name = stored.Name
		}
	}
	if request.Color != nil {
		label.Color = *request.Color
	}
	if request.OrderIndex != nil {
		label.OrderIndex = *request.OrderIndex
	}

	patchInfo := appstate.BuildLabelEdit(label.ID, label.Name, label.Color, label.Deleted)
	action := patchInfo.Mutations[0].Value.GetLabelEditAction()
	if request.OrderIndex != nil || (stored != nil && stored.OrderIndex != 0) {
		action.OrderIndex = proto.Int32(label.OrderIndex)
	}
	if value, ok := waSyncAction.LabelEditAction_ListType_value[label.Type]; ok && label.Type != "" {
		listType := waSyncAction.LabelEditAction_ListType(value)
		action.Type = &listType
		action.IsActive = proto.Bool(true)
	}
	return patchInfo, label
}

func (service *serviceLabel) LabelChat(ctx context.Context, request domainLabel.LabelChatRequest) (response domainLabel.LabelChatResponse, err error) {
	if err = validations.ValidateLabelChat(ctx, &request); err != nil {
		return response, err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return response, pkgError.ErrWaCLI
	}

	// Validate JID and ensure connection
	targetJID, err := utils.ValidateJidWithLogin(client, request.ChatJID)
	if err != nil {
		return response, err
	}

	labeled := *request.Labeled
	if err = client.SendAppState(ctx, appstate.BuildLabelChat(targetJID, request.LabelID, labeled)); err != nil {
		logrus.WithError(err).WithFields(logrus.Fields{
			"chat_jid": request.ChatJID,
			"label_id": request.LabelID,
		}).Error("Failed to send label chat app state")
		return response, err
	}

	if deviceID := storageDeviceID(ctx); deviceID != "" {
		chatJID := whatsapp.NormalizeJIDFromLID(ctx, targetJID, client).ToNonAD().String()
		if storeErr := service.chatStorageRepo.SetLabelChat(deviceID, request.LabelID, chatJID, labeled, time.Now()); storeErr != nil {
			logrus.WithError(storeErr).WithField("label_id", request.LabelID).Warn("Failed to store label chat")
		}
	}

	response.Status = "success"
	response.ChatJID = request.ChatJID
	response.LabelID = request.LabelID
	response.Labeled = labeled
	if labeled {
		response.Message = "Chat added to label successfully"
	} else {
		response.Message = "Chat removed from label successfully"
	}
	return response, nil
}
