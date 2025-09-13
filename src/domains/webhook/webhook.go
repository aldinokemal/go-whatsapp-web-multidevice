package webhook

import (
	"time"
)

type Webhook struct {
	ID          string    `json:"id"`
	URL         string    `json:"url"`
	Secret      string    `json:"secret,omitempty"`
	Events      []string  `json:"events"`
	Enabled     bool      `json:"enabled"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type CreateWebhookRequest struct {
	URL         string   `json:"url" validate:"required,url"`
	Secret      string   `json:"secret"`
	Events      []string `json:"events" validate:"required,min=1,dive,oneof=message message.ack group group.join group.leave group.promote group.demote message.delete presence"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
}

type UpdateWebhookRequest struct {
	URL         string   `json:"url" validate:"required,url"`
	Secret      string   `json:"secret"`
	Events      []string `json:"events" validate:"required,min=1,dive,oneof=message message.ack group group.join group.leave group.promote group.demote message.delete presence"`
	Description string   `json:"description"`
	Enabled     bool     `json:"enabled"`
}

var ValidEvents = []string{
	"message",
	"message.ack", 
	"group",
	"group.join",
	"group.leave",
	"group.promote",
	"group.demote",
	"message.delete",
	"presence",
}