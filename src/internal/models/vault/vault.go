package models

import "time"

// VaultCredential represents an encrypted key-value entry in public.vault.
type VaultCredential struct {
	CredentialID   string    `json:"credential_id"`
	Key            string    `json:"key"`
	Value          string    `json:"value,omitempty"`
	EncryptedValue string    `json:"-"`
	EncryptedDEK   string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	CreatedBy      *string   `json:"created_by"`
	OrganizationID *string   `json:"organization_id,omitempty"`
	ProjectID      *string   `json:"project_id,omitempty"`
}
