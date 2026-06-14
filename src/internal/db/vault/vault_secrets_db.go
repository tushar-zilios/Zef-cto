package vault

import (
	"context"

	"cto/src/internal/db"
	models "cto/src/internal/models/vault"
)

func CreateVaultCredential(ctx context.Context, c *models.VaultCredential) error {
	query := `
		INSERT INTO public.vault (key, value, encryption_key, created_by, organization_id, project_id)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING credential_id, created_at
	`
	return db.GetCTOPoolOrNil().QueryRow(ctx, query,
		c.Key, c.EncryptedValue, c.EncryptedDEK, c.CreatedBy, c.OrganizationID, c.ProjectID,
	).Scan(&c.CredentialID, &c.CreatedAt)
}

func GetVaultCredentialByID(ctx context.Context, id string) (*models.VaultCredential, error) {
	query := `
		SELECT credential_id, key, value, encryption_key, created_at, created_by, organization_id, project_id
		FROM public.vault WHERE credential_id = $1
	`
	c := &models.VaultCredential{}
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, id).Scan(
		&c.CredentialID, &c.Key, &c.EncryptedValue, &c.EncryptedDEK, &c.CreatedAt, &c.CreatedBy, &c.OrganizationID, &c.ProjectID,
	)
	return c, err
}

func UpdateVaultCredential(ctx context.Context, c *models.VaultCredential) error {
	query := `
		UPDATE public.vault
		SET value = $1, encryption_key = $2
		WHERE credential_id = $3
	`
	_, err := db.GetCTOPoolOrNil().Exec(ctx, query, c.EncryptedValue, c.EncryptedDEK, c.CredentialID)
	return err
}

func DeleteVaultCredential(ctx context.Context, id string) error {
	_, err := db.GetCTOPoolOrNil().Exec(ctx, `DELETE FROM public.vault WHERE credential_id = $1`, id)
	return err
}

func ListVaultCredentials(ctx context.Context, orgID, projectID, search string, limit, offset int) ([]*models.VaultCredential, error) {
	query := `
		SELECT credential_id, key, value, encryption_key, created_at, created_by, organization_id, project_id
		FROM public.vault
		WHERE ($1 = '' OR organization_id::text = $1)
		  AND ($2 = '' OR project_id::text = $2)
		  AND ($3 = '' OR key ILIKE '%' || $3 || '%')
		ORDER BY created_at DESC
		LIMIT $4 OFFSET $5
	`
	rows, err := db.GetCTOPoolOrNil().Query(ctx, query, orgID, projectID, search, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*models.VaultCredential
	for rows.Next() {
		c := &models.VaultCredential{}
		if err := rows.Scan(
			&c.CredentialID, &c.Key, &c.EncryptedValue, &c.EncryptedDEK, &c.CreatedAt, &c.CreatedBy, &c.OrganizationID, &c.ProjectID,
		); err != nil {
			return nil, err
		}
		list = append(list, c)
	}
	return list, rows.Err()
}
