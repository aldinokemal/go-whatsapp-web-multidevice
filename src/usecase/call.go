package usecase

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type serviceCall struct {
	chatStorageRepo domainChatStorage.IChatStorageRepository
}

func NewCallService(chatStorageRepo domainChatStorage.IChatStorageRepository) domainCall.ICallUsecase {
	return &serviceCall{
		chatStorageRepo: chatStorageRepo,
	}
}

func (service serviceCall) RejectCall(ctx context.Context, callerJID string, callID string) error {
	if err := validations.ValidateRejectCall(ctx, callerJID, callID); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	utils.MustLogin(client)
	parsedJID, err := utils.ParseJID(callerJID)
	if err != nil {
		return err
	}

	rejectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.RejectCall(rejectCtx, parsedJID, callID); err != nil {
		logrus.WithError(err).Error("Failed to reject call")
		return fmt.Errorf("failed to reject call: %w", err)
	}

	logrus.Info("Rejected call successfully")
	return nil
}

// ListCallLogs returns the call records persisted by the incoming call handler
// for the device in context, newest first.
func (service serviceCall) ListCallLogs(ctx context.Context, request domainCall.ListCallLogsRequest) (response domainCall.ListCallLogsResponse, err error) {
	if err = validations.ValidateListCallLogs(ctx, &request); err != nil {
		return response, err
	}

	deviceID := deviceIDFromContext(ctx)
	if deviceID == "" {
		return response, fmt.Errorf("device identification required")
	}

	if service.chatStorageRepo == nil {
		return response, pkgError.InternalServerError("chat storage repository is not available")
	}

	records, total, err := service.chatStorageRepo.GetCallRecords(&domainChatStorage.CallRecordFilter{
		DeviceID: deviceID,
		ChatJID:  strings.TrimSpace(request.ChatJID),
		Limit:    request.Limit,
		Offset:   request.Offset,
	})
	if err != nil {
		logrus.WithError(err).Error("Failed to get call records")
		return response, err
	}

	callLogs := make([]domainCall.CallLogInfo, 0, len(records))
	for _, record := range records {
		callLog := domainCall.CallLogInfo{
			ID:        record.ID,
			ChatJID:   record.ChatJID,
			SenderJID: record.Sender,
			Timestamp: record.Timestamp.Format(time.RFC3339),
			IsFromMe:  record.IsFromMe,
		}
		if record.CallMetadata != "" {
			if err := json.Unmarshal([]byte(record.CallMetadata), &callLog.CallMetadata); err != nil {
				logrus.WithError(err).WithField("message_id", record.ID).Warn("Failed to parse call metadata")
			}
		}
		callLogs = append(callLogs, callLog)
	}

	response.Data = callLogs
	response.Pagination = domainCall.PaginationResponse{
		Limit:  request.Limit,
		Offset: request.Offset,
		Total:  int(total),
	}

	return response, nil
}
