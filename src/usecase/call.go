package usecase

import (
	"context"
	"fmt"
	"time"

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

	parsedJID, err := utils.ValidateJidWithLogin(client, callerJID)
	if err != nil {
		return err
	}

	rejectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := client.RejectCall(rejectCtx, parsedJID, callID); err != nil {
		logrus.Errorf("Failed to reject call from %s (CallID: %s): %v", callerJID, callID, err)
		return fmt.Errorf("failed to reject call: %w", err)
	}

	logrus.Infof("Rejected call from %s (CallID: %s)", callerJID, callID)
	return nil
}
