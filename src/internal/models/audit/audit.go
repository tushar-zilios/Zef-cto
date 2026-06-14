package models

import "time"

type ActionType string

const (
	ActionCreate ActionType = "create"
	ActionRead   ActionType = "read"
	ActionUpdate ActionType = "update"
	ActionDelete ActionType = "delete"
)

type Resource struct {
	ResourceID string  `json:"resource_id"`
	Name       *string `json:"name"`
	ParentID   *string `json:"parent_id"`
}

type AuditLog struct {
	LogID          string     `json:"log_id"`
	Action         ActionType `json:"action"`
	ResourceID     *string    `json:"resource_id"`
	ResourceName   *string    `json:"resource_name,omitempty"`
	UserID         *string    `json:"user_id"`
	CreatedAt      time.Time  `json:"created_at"`
	OrganizationID *string    `json:"organization_id,omitempty"`
	ProjectID      *string    `json:"project_id,omitempty"`
}
