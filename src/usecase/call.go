package usecase

import (
	"context"
	"fmt"
	"time"

	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	domainChatStorage "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/chatstorage"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type CallRuntime interface {
	StartCall(ctx context.Context, device *whatsapp.DeviceInstance, phone string, record bool) (domainCall.CallInfo, error)
	AcceptCall(ctx context.Context, deviceID, callID string, record bool) (domainCall.CallInfo, error)
	RejectCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, error)
	EndCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, error)
	ExchangeWebRTC(ctx context.Context, deviceID, callID, sdpOffer string) (string, error)
	GetCall(ctx context.Context, deviceID, callID string) (domainCall.CallInfo, bool)
	ListCalls(ctx context.Context, deviceID string) []domainCall.CallInfo
}

type serviceCall struct {
	runtime CallRuntime
	storage domainChatStorage.IChatStorageRepository
}

func NewCallService(deps ...any) domainCall.ICallUsecase {
	service := &serviceCall{}
	for _, dep := range deps {
		switch typed := dep.(type) {
		case CallRuntime:
			service.runtime = typed
		case domainChatStorage.IChatStorageRepository:
			service.storage = typed
		}
	}
	return service
}

func callDeviceFromContext(ctx context.Context) (*whatsapp.DeviceInstance, string, error) {
	device, ok := whatsapp.DeviceFromContext(ctx)
	if !ok || device == nil || device.ID() == "" {
		return nil, "", fmt.Errorf("device identification required")
	}
	return device, device.ID(), nil
}

func (service *serviceCall) StartCall(ctx context.Context, request domainCall.StartCallRequest) (domainCall.StartCallResponse, error) {
	if service.runtime == nil {
		return domainCall.StartCallResponse{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateStartCall(ctx, request); err != nil {
		return domainCall.StartCallResponse{}, err
	}
	device, _, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.StartCallResponse{}, err
	}
	info, err := service.runtime.StartCall(ctx, device, request.Phone, request.Record)
	if err != nil {
		return domainCall.StartCallResponse{}, err
	}
	return domainCall.StartCallResponse{Call: info}, nil
}

func (service *serviceCall) AcceptCall(ctx context.Context, request domainCall.CallIDRequest) (domainCall.GenericResponse, error) {
	if service.runtime == nil {
		return domainCall.GenericResponse{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateCallIDRequest(ctx, request); err != nil {
		return domainCall.GenericResponse{}, err
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	info, err := service.runtime.AcceptCall(ctx, deviceID, request.CallID, request.Record)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	return domainCall.GenericResponse{Status: "success", Call: info}, nil
}

func (service *serviceCall) RejectCall(ctx context.Context, request domainCall.CallIDRequest) (domainCall.GenericResponse, error) {
	if service.runtime == nil {
		return domainCall.GenericResponse{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateCallIDRequest(ctx, request); err != nil {
		return domainCall.GenericResponse{}, err
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	info, err := service.runtime.RejectCall(ctx, deviceID, request.CallID)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	return domainCall.GenericResponse{Status: "success", Call: info}, nil
}

func (service *serviceCall) RejectIncomingCall(ctx context.Context, request domainCall.RejectCallRequest) error {
	if err := validations.ValidateRejectCall(ctx, request.CallerJID, request.CallID); err != nil {
		return err
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return pkgError.ErrWaCLI
	}

	utils.MustLogin(client)
	parsedJID, err := utils.ParseJID(request.CallerJID)
	if err != nil {
		return err
	}

	rejectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.RejectCall(rejectCtx, parsedJID, request.CallID); err != nil {
		logrus.WithError(err).Error("Failed to reject call")
		return fmt.Errorf("failed to reject call: %w", err)
	}

	logrus.Info("Rejected call successfully")
	return nil
}

func (service *serviceCall) EndCall(ctx context.Context, request domainCall.CallIDRequest) (domainCall.GenericResponse, error) {
	if service.runtime == nil {
		return domainCall.GenericResponse{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateCallIDRequest(ctx, request); err != nil {
		return domainCall.GenericResponse{}, err
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	info, err := service.runtime.EndCall(ctx, deviceID, request.CallID)
	if err != nil {
		return domainCall.GenericResponse{}, err
	}
	return domainCall.GenericResponse{Status: "success", Call: info}, nil
}

func (service *serviceCall) ExchangeWebRTC(ctx context.Context, request domainCall.WebRTCRequest) (domainCall.WebRTCResponse, error) {
	if service.runtime == nil {
		return domainCall.WebRTCResponse{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateWebRTCRequest(ctx, request); err != nil {
		return domainCall.WebRTCResponse{}, err
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.WebRTCResponse{}, err
	}
	answer, err := service.runtime.ExchangeWebRTC(ctx, deviceID, request.CallID, request.SDPOffer)
	if err != nil {
		return domainCall.WebRTCResponse{}, err
	}
	return domainCall.WebRTCResponse{CallID: request.CallID, SDPAnswer: answer}, nil
}

func (service *serviceCall) GetCall(ctx context.Context, request domainCall.CallIDRequest) (domainCall.CallInfo, error) {
	if service.runtime == nil {
		return domainCall.CallInfo{}, fmt.Errorf("call runtime is not configured")
	}
	if err := validations.ValidateCallIDRequest(ctx, request); err != nil {
		return domainCall.CallInfo{}, err
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.CallInfo{}, err
	}
	info, ok := service.runtime.GetCall(ctx, deviceID, request.CallID)
	if !ok {
		if service.storage == nil {
			return domainCall.CallInfo{}, fmt.Errorf("call %s not found", request.CallID)
		}
		record, err := service.storage.GetCallRecord(deviceID, request.CallID)
		if err != nil || record == nil {
			return domainCall.CallInfo{}, fmt.Errorf("call %s not found", request.CallID)
		}
		return callInfoFromRecord(record), nil
	}
	return info, nil
}

func (service *serviceCall) ListCalls(ctx context.Context) (domainCall.ListCallsResponse, error) {
	if service.runtime == nil {
		return domainCall.ListCallsResponse{}, fmt.Errorf("call runtime is not configured")
	}
	_, deviceID, err := callDeviceFromContext(ctx)
	if err != nil {
		return domainCall.ListCallsResponse{}, err
	}
	if service.storage != nil {
		records, err := service.storage.ListCallRecords(deviceID, 100)
		if err != nil {
			return domainCall.ListCallsResponse{}, err
		}
		return domainCall.ListCallsResponse{Data: callInfosFromRecords(records)}, nil
	}
	return domainCall.ListCallsResponse{Data: service.runtime.ListCalls(ctx, deviceID)}, nil
}

func callInfoFromRecord(record *domainChatStorage.CallRecord) domainCall.CallInfo {
	info := domainCall.CallInfo{
		DeviceID:  record.DeviceID,
		CallID:    record.CallID,
		PeerJID:   record.PeerJID,
		Direction: record.Direction,
		Status:    record.Status,
		MediaType: record.MediaType,
		StartedAt: record.StartedAt,
		UpdatedAt: record.UpdatedAt,
		EndReason: record.EndReason,
		Metadata:  record.Metadata,
	}
	if record.EndedAt != nil {
		info.EndedAt = *record.EndedAt
	}
	whatsapp.ApplyCallRecordingMetadata(&info, record.Metadata)
	return info
}

func callInfosFromRecords(records []*domainChatStorage.CallRecord) []domainCall.CallInfo {
	result := make([]domainCall.CallInfo, 0, len(records))
	for _, record := range records {
		result = append(result, callInfoFromRecord(record))
	}
	return result
}
