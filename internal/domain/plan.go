package domain

import "time"

type PlanLimits struct {
	Seats int `json:"seats"`
}

type Plan struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"tenant_id"`
	ProductID   *string    `json:"product_id,omitempty"`
	Name        string     `json:"name"`
	Features    []string   `json:"features"`
	Limits      PlanLimits `json:"limits"`
	IsActive    bool       `json:"is_active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

