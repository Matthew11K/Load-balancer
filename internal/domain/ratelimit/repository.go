package ratelimit

import "time"

type ClientData struct {
	ID         string    `json:"id"`
	Capacity   int       `json:"capacity"`
	RatePerSec float64   `json:"rate_per_sec"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type Repository interface {
	SaveClient(clientData *ClientData) error
	GetClient(id string) (*ClientData, error)
	DeleteClient(id string) error
	ListClients() ([]*ClientData, error)
}
