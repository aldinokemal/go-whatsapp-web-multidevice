package usecase

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/config"
	domainCall "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/call"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/infrastructure/whatsapp"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/utils"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type serviceCall struct{}

func NewCallService() domainCall.ICallUsecase {
	return &serviceCall{}
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

func (service serviceCall) StartCall(ctx context.Context, request domainCall.StartCallRequest) (domainCall.StartCallResponse, error) {
	if err := validations.ValidateStartCall(ctx, request); err != nil {
		return domainCall.StartCallResponse{}, err
	}

	caller, err := betaCallerFromContext(ctx)
	if err != nil {
		return domainCall.StartCallResponse{}, err
	}

	audioFile := strings.TrimSpace(request.AudioFilePath)
	recordPath := strings.TrimSpace(request.RecordPath)
	startCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	callID, err := caller.StartCall(
		startCtx,
		strings.TrimSpace(request.Target),
		audioFile,
		recordPath,
	)
	if err != nil {
		logrus.WithError(err).Error("Failed to start call")
		return domainCall.StartCallResponse{}, err
	}

	return domainCall.StartCallResponse{
		CallID: callID,
		Target: strings.TrimSpace(request.Target),
	}, nil
}

func (service serviceCall) AnswerCall(ctx context.Context, callID string, recordPath string) error {
	if err := validations.ValidateCallID(ctx, callID); err != nil {
		return err
	}

	caller, err := betaCallerFromContext(ctx)
	if err != nil {
		return err
	}

	if err := caller.AnswerCall(strings.TrimSpace(callID), strings.TrimSpace(recordPath)); err != nil {
		logrus.WithError(err).Error("Failed to answer call")
		return betaCallError(err)
	}

	return nil
}

// AttachAudio bridges a live call to the browser audio stream.
func (service serviceCall) AttachAudio(
	ctx context.Context,
	callID string,
	mic <-chan []byte,
	onPeerAudio func([]float32),
) error {
	if err := validations.ValidateCallID(ctx, callID); err != nil {
		return err
	}

	caller, err := betaCallerFromContext(ctx)
	if err != nil {
		return err
	}

	if err := caller.AttachAudioStream(strings.TrimSpace(callID), mic, onPeerAudio); err != nil {
		return betaCallError(err)
	}

	return nil
}

// WatchCallEnd registers fn to run when the call ends.
func (service serviceCall) WatchCallEnd(ctx context.Context, callID string, fn func()) error {
	if err := validations.ValidateCallID(ctx, callID); err != nil {
		return err
	}

	caller, err := betaCallerFromContext(ctx)
	if err != nil {
		return err
	}

	caller.WatchCallEnd(strings.TrimSpace(callID), fn)
	return nil
}

func (service serviceCall) HangupCall(ctx context.Context, callID string) error {
	if err := validations.ValidateCallID(ctx, callID); err != nil {
		return err
	}

	caller, err := betaCallerFromContext(ctx)
	if err != nil {
		return err
	}

	if err := caller.HangupCall(strings.TrimSpace(callID)); err != nil {
		logrus.WithError(err).Error("Failed to hang up call")
		return betaCallError(err)
	}

	return nil
}

// betaCallError maps infrastructure call errors onto API-facing error types.
func betaCallError(err error) error {
	if errors.Is(err, whatsapp.ErrCallNotFound) {
		return pkgError.ErrCallNotFound
	}
	return err
}

// betaCallerFromContext resolves the beta call stack for the device scoped to the
// request, returning the same errors the other call endpoints surface.
func betaCallerFromContext(ctx context.Context) (*whatsapp.Caller, error) {
	if !config.WhatsappBetaCallEnabled {
		return nil, pkgError.ValidationError(
			"beta voice calls are disabled; set WHATSAPP_BETA_CALL_ENABLED=true",
		)
	}

	client := whatsapp.ClientFromContext(ctx)
	if client == nil {
		return nil, pkgError.ErrWaCLI
	}
	utils.MustLogin(client)

	// Prefer the slot: it carries the stable session identities (AD JID first)
	// that attach used, even after the registry re-keyed the device.
	if instance, ok := whatsapp.DeviceFromContext(ctx); ok && instance != nil {
		if caller := whatsapp.CallerForInstance(instance); caller != nil {
			return caller, nil
		}
	} else if caller := whatsapp.CallerForDevice(""); caller != nil {
		// The startup client may have no paired identity yet.
		return caller, nil
	}

	return nil, pkgError.ValidationError(
		"beta voice calls are not available for this device; reconnect it and retry",
	)
}
