package webhook

type IWebhookRepository interface {
	Create(webhook *Webhook) error
	FindAll() ([]*Webhook, error)
	FindByID(id string) (*Webhook, error)
	Update(webhook *Webhook) error
	Delete(id string) error
	FindByEvent(event string) ([]*Webhook, error)
	FindEnabled() ([]*Webhook, error)
	InitializeSchema() error
}