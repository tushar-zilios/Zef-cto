package models

import "time"

type Organization struct {
	OrganizationID   string    `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`
	Slug             *string   `json:"slug,omitempty"`
	LogoURL          *string   `json:"logo_url,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
