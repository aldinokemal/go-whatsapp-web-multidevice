package usecase

import (
	"fmt"

	"github.com/aldinokemal/go-whatsapp-web-multidevice/domains/webhook"
	"github.com/aldinokemal/go-whatsapp-web-multidevice/validations"
	"github.com/sirupsen/logrus"
)

type webhookService struct {
	repo webhook.IWebhookRepository
}

func NewWebhookService(repo webhook.IWebhookRepository) webhook.IWebhookUsecase {
	return &webhookService{repo: repo}
}

func (s *webhookService) CreateWebhook(request *webhook.CreateWebhookRequest) error {
	if err := validations.ValidateCreateWebhook(request); err != nil {
		logrus.Errorf("Validation error when creating webhook: %s", err.Error())
		return err
	}

	wh := &webhook.Webhook{
		URL:         request.URL,
		Secret:      request.Secret,
		Events:      request.Events,
		Enabled:     request.Enabled,
		Description: request.Description,
	}

	return s.repo.Create(wh)
}

func (s *webhookService) GetAllWebhooks() ([]*webhook.Webhook, error) {
	return s.repo.FindAll()
}

func (s *webhookService) GetWebhookByID(id string) (*webhook.Webhook, error) {
	if id == "" {
		return nil, fmt.Errorf("webhook ID is required")
	}
	
	return s.repo.FindByID(id)
}

func (s *webhookService) UpdateWebhook(id string, request *webhook.UpdateWebhookRequest) error {
	if id == "" {
		return fmt.Errorf("webhook ID is required")
	}

	if err := validations.ValidateUpdateWebhook(request); err != nil {
		logrus.Errorf("Validation error when updating webhook: %s", err.Error())
		return err
	}

	existing, err := s.repo.FindByID(id)
	if err != nil {
		return fmt.Errorf("webhook not found: %w", err)
	}

	existing.URL = request.URL
	existing.Secret = request.Secret
	existing.Events = request.Events
	existing.Enabled = request.Enabled
	existing.Description = request.Description

	return s.repo.Update(existing)
}

func (s *webhookService) DeleteWebhook(id string) error {
	if id == "" {
		return fmt.Errorf("webhook ID is required")
	}

	return s.repo.Delete(id)
}

func (s *webhookService) GetWebhooksByEvent(event string) ([]*webhook.Webhook, error) {
	if event == "" {
		return nil, fmt.Errorf("event is required")
	}

	return s.repo.FindByEvent(event)
}

func (s *webhookService) GetEnabledWebhooks() ([]*webhook.Webhook, error) {
	return s.repo.FindEnabled()
}