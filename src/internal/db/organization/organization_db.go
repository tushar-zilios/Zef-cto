package organization

import (
	"context"

	"cto/src/internal/db"
	models "cto/src/internal/models/organization"
)

func UpsertOrganization(ctx context.Context, org *models.Organization) error {
	query := `
		INSERT INTO public.organizations (organization_id, organization_name, slug, logo_url)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (organization_id) DO UPDATE
			SET organization_name = EXCLUDED.organization_name,
			    slug              = COALESCE(EXCLUDED.slug, public.organizations.slug),
			    logo_url          = COALESCE(EXCLUDED.logo_url, public.organizations.logo_url),
			    updated_at        = NOW()
		RETURNING created_at, updated_at
	`
	return db.GetCTOPoolOrNil().QueryRow(ctx, query,
		org.OrganizationID, org.OrganizationName, org.Slug, org.LogoURL,
	).Scan(&org.CreatedAt, &org.UpdatedAt)
}

func GetOrganizationByID(ctx context.Context, id string) (*models.Organization, error) {
	query := `
		SELECT organization_id, organization_name, slug, logo_url, created_at, updated_at
		FROM public.organizations WHERE organization_id = $1
	`
	o := &models.Organization{}
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, id).Scan(
		&o.OrganizationID, &o.OrganizationName, &o.Slug, &o.LogoURL, &o.CreatedAt, &o.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return o, nil
}
