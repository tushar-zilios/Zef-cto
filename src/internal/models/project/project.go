package models

import "time"

type Project struct {
	ProjectID      string    `json:"project_id"`
	Name           string    `json:"name"`
	OrganizationID *string   `json:"organization_id,omitempty"`
	CreatedBy      *string   `json:"created_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}
