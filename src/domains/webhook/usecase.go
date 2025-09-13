package webhook

type IWebhookUsecase interface {
	CreateWebhook(request *CreateWebhookRequest) error
	GetAllWebhooks() ([]*Webhook, error)
	GetWebhookByID(id string) (*Webhook, error)
	UpdateWebhook(id string, request *UpdateWebhookRequest) error
	DeleteWebhook(id string) error
	GetWebhooksByEvent(event string) ([]*Webhook, error)
	GetEnabledWebhooks() ([]*Webhook, error)
}