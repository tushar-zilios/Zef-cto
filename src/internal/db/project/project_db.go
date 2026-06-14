package project

import (
	"context"

	"cto/src/internal/db"
	models "cto/src/internal/models/project"
)

func CreateProject(ctx context.Context, p *models.Project) error {
	query := `
		INSERT INTO public.projects (name, organization_id, created_by)
		VALUES ($1, $2, $3)
		RETURNING project_id, created_at
	`
	return db.GetCTOPoolOrNil().QueryRow(ctx, query, p.Name, p.OrganizationID, p.CreatedBy).
		Scan(&p.ProjectID, &p.CreatedAt)
}

func ListProjects(ctx context.Context, orgID string) ([]*models.Project, error) {
	query := `
		SELECT project_id, name, organization_id, created_by, created_at
		FROM public.projects
		WHERE ($1 = '' OR organization_id::text = $1)
		ORDER BY created_at DESC
	`
	rows, err := db.GetCTOPoolOrNil().Query(ctx, query, orgID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []*models.Project
	for rows.Next() {
		p := &models.Project{}
		if err := rows.Scan(&p.ProjectID, &p.Name, &p.OrganizationID, &p.CreatedBy, &p.CreatedAt); err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}

func GetProject(ctx context.Context, id string) (*models.Project, error) {
	query := `
		SELECT project_id, name, organization_id, created_by, created_at
		FROM public.projects WHERE project_id = $1
	`
	p := &models.Project{}
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, id).
		Scan(&p.ProjectID, &p.Name, &p.OrganizationID, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return p, nil
}

func DeleteProject(ctx context.Context, id string) error {
	_, err := db.GetCTOPoolOrNil().Exec(ctx, `DELETE FROM public.projects WHERE project_id = $1`, id)
	return err
}
