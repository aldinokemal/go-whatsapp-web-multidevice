package usecase

import (
	"context"
	"database/sql"
	"errors"
	"time"

	domainSchedule "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/schedule"
	domainSend "github.com/aldinokemal/go-whatsapp-web-multidevice/domains/send"
	pkgError "github.com/aldinokemal/go-whatsapp-web-multidevice/pkg/error"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
)

const manualRunMaxErrorLength = 512

type serviceSchedule struct {
	repo   domainSchedule.IScheduledMessageRepository
	sender domainSend.ISendUsecase
}

// NewScheduleService constructs the schedule management usecase. Returns nil if repository is nil.
func NewScheduleService(repo domainSchedule.IScheduledMessageRepository, sender domainSend.ISendUsecase) domainSchedule.IScheduleUsecase {
	if repo == nil {
		return nil
	}

	return &serviceSchedule{
		repo:   repo,
		sender: sender,
	}
}

func (s *serviceSchedule) List(ctx context.Context, request domainSchedule.ListScheduledMessagesRequest) ([]*domainSchedule.ScheduledMessage, error) {
	filter := domainSchedule.ListFilter{
		Statuses: request.Statuses,
		Limit:    request.Limit,
		Offset:   request.Offset,
	}

	return s.repo.List(ctx, filter)
}

func (s *serviceSchedule) Get(ctx context.Context, id int64) (*domainSchedule.ScheduledMessage, error) {
	message, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pkgError.ValidationError("scheduled message not found")
		}
		return nil, err
	}
	return message, nil
}

func (s *serviceSchedule) Create(ctx context.Context, payload domainSchedule.ScheduleMessagePayload) (*domainSchedule.ScheduledMessage, error) {
	if payload.ScheduleAt == nil {
		return nil, pkgError.ValidationError("schedule_at is required")
	}

	if s.sender == nil {
		return nil, pkgError.InternalServerError("scheduled messaging is not configured")
	}

	request := domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       payload.Phone,
			Duration:    payload.Duration,
			IsForwarded: payload.IsForwarded,
		},
		Message:        payload.Message,
		ReplyMessageID: payload.ReplyMessageID,
		ScheduleAt:     payload.ScheduleAt,
	}

	if err := validations.ValidateSendMessage(ctx, request); err != nil {
		return nil, err
	}

	response, err := s.sender.SendText(ctx, request)
	if err != nil {
		return nil, err
	}

	if response.ScheduledID == nil {
		return nil, pkgError.InternalServerError("scheduled message identifier missing from response")
	}

	return s.repo.GetByID(ctx, *response.ScheduledID)
}

func (s *serviceSchedule) Update(ctx context.Context, id int64, payload domainSchedule.ScheduleMessagePayload) (*domainSchedule.ScheduledMessage, error) {
	if payload.ScheduleAt == nil {
		return nil, pkgError.ValidationError("schedule_at is required")
	}

	request := domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       payload.Phone,
			Duration:    payload.Duration,
			IsForwarded: payload.IsForwarded,
		},
		Message:        payload.Message,
		ReplyMessageID: payload.ReplyMessageID,
		ScheduleAt:     payload.ScheduleAt,
	}

	if err := validations.ValidateSendMessage(ctx, request); err != nil {
		return nil, err
	}

	update := domainSchedule.ScheduledMessageUpdate{
		Phone:          payload.Phone,
		Message:        payload.Message,
		ReplyMessageID: payload.ReplyMessageID,
		IsForwarded:    payload.IsForwarded,
		Duration:       payload.Duration,
		ScheduleAt:     payload.ScheduleAt.UTC(),
	}

	message, err := s.repo.Update(ctx, id, update)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pkgError.ValidationError("scheduled message cannot be updated (not found or already processed)")
		}
		return nil, err
	}

	return message, nil
}

func (s *serviceSchedule) Delete(ctx context.Context, id int64) error {
	if err := s.repo.Delete(ctx, id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return pkgError.ValidationError("scheduled message cannot be deleted (not found or already processed)")
		}
		return err
	}
	return nil
}

func (s *serviceSchedule) RunNow(ctx context.Context, id int64) (*domainSchedule.ScheduledMessage, error) {
	message, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, pkgError.ValidationError("scheduled message not found")
		}
		return nil, err
	}

	if message.Status != domainSchedule.StatusPending {
		return nil, pkgError.ValidationError("only pending messages can be dispatched immediately")
	}

	acquired, err := s.repo.MarkProcessing(ctx, id)
	if err != nil {
		return nil, err
	}
	if !acquired {
		return nil, pkgError.ValidationError("scheduled message is already being processed")
	}

	request := domainSend.MessageRequest{
		BaseRequest: domainSend.BaseRequest{
			Phone:       message.Phone,
			Duration:    message.Duration,
			IsForwarded: message.IsForwarded,
		},
		Message:        message.Message,
		ReplyMessageID: message.ReplyMessageID,
	}

	if err := validations.ValidateSendMessage(ctx, request); err != nil {
		_ = s.repo.MarkFailed(ctx, id, truncateError(err.Error()))
		return nil, err
	}

	response, err := s.sender.SendText(ctx, request)
	if err != nil {
		_ = s.repo.MarkFailed(ctx, id, truncateError(err.Error()))
		return nil, err
	}

	if response.MessageID == "" {
		response.MessageID = "manual-dispatch"
	}

	if err := s.repo.MarkSent(ctx, id, response.MessageID, time.Now().UTC()); err != nil {
		return nil, err
	}

	return s.repo.GetByID(ctx, id)
}

func truncateError(msg string) string {
    if len(msg) <= manualRunMaxErrorLength {
        return msg
    }
    runes := []rune(msg)
    if len(runes) <= manualRunMaxErrorLength {
        return msg
    }
    return string(runes[:manualRunMaxErrorLength])
}
