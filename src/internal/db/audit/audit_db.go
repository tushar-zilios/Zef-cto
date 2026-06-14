package db

import (
	"context"
	"strconv"

	"cto/src/internal/db"
	models "cto/src/internal/models/audit"
)

// --- Resources ---

func CreateResource(ctx context.Context, r *models.Resource) error {
	query := `
		INSERT INTO public.resources (name, parent_id)
		VALUES ($1, $2)
		RETURNING resource_id
	`
	return db.GetCTOPoolOrNil().QueryRow(ctx, query, r.Name, r.ParentID).Scan(&r.ResourceID)
}

func GetResourceByID(ctx context.Context, id string) (*models.Resource, error) {
	query := `SELECT resource_id, name, parent_id FROM public.resources WHERE resource_id = $1`
	r := &models.Resource{}
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, id).Scan(&r.ResourceID, &r.Name, &r.ParentID)
	if err != nil {
		return nil, err
	}
	return r, nil
}

func ListResources(ctx context.Context) ([]*models.Resource, error) {
	query := `SELECT resource_id, name, parent_id FROM public.resources ORDER BY name`
	rows, err := db.GetCTOPoolOrNil().Query(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var resources []*models.Resource
	for rows.Next() {
		r := &models.Resource{}
		if err := rows.Scan(&r.ResourceID, &r.Name, &r.ParentID); err != nil {
			return nil, err
		}
		resources = append(resources, r)
	}
	return resources, nil
}

func UpdateResource(ctx context.Context, r *models.Resource) error {
	query := `UPDATE public.resources SET name = $1, parent_id = $2 WHERE resource_id = $3`
	_, err := db.GetCTOPoolOrNil().Exec(ctx, query, r.Name, r.ParentID, r.ResourceID)
	return err
}

func DeleteResource(ctx context.Context, id string) error {
	query := `DELETE FROM public.resources WHERE resource_id = $1`
	_, err := db.GetCTOPoolOrNil().Exec(ctx, query, id)
	return err
}

// --- Audit Logs ---

func CreateAuditLog(ctx context.Context, log *models.AuditLog) error {
	query := `
		INSERT INTO public.audit_logs (action, resource_id, user_id, organization_id, project_id)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING log_id, created_at
	`
	return db.GetCTOPoolOrNil().QueryRow(ctx, query,
		log.Action, log.ResourceID, log.UserID, log.OrganizationID, log.ProjectID,
	).Scan(&log.LogID, &log.CreatedAt)
}

func GetAuditLogByID(ctx context.Context, id string) (*models.AuditLog, error) {
	query := `
		SELECT al.log_id, al.action, al.resource_id, r.name, al.user_id, al.created_at, al.organization_id, al.project_id
		FROM public.audit_logs al
		LEFT JOIN public.resources r ON r.resource_id = al.resource_id
		WHERE al.log_id = $1
	`
	l := &models.AuditLog{}
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, id).
		Scan(&l.LogID, &l.Action, &l.ResourceID, &l.ResourceName, &l.UserID, &l.CreatedAt, &l.OrganizationID, &l.ProjectID)
	if err != nil {
		return nil, err
	}
	return l, nil
}

type AuditFilter struct {
	ResourceID     string
	Action         string
	OrganizationID string
	ProjectID      string
	Search         string // matches against action text
	SortDesc       bool
}

func ListAuditLogs(ctx context.Context, f AuditFilter, limit, offset int) ([]*models.AuditLog, error) {
	order := "DESC"
	if !f.SortDesc {
		order = "ASC"
	}

	args := []any{}
	where := buildAuditWhere(f, &args)
	n := len(args)

	args = append(args, limit, offset)
	query := `
		SELECT al.log_id, al.action, al.resource_id, r.name, al.user_id, al.created_at, al.organization_id, al.project_id
		FROM public.audit_logs al
		LEFT JOIN public.resources r ON r.resource_id = al.resource_id
	` + where + ` ORDER BY al.created_at ` + order + ` LIMIT $` + itoa(n+1) + ` OFFSET $` + itoa(n+2)

	rows, err := db.GetCTOPoolOrNil().Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var logs []*models.AuditLog
	for rows.Next() {
		l := &models.AuditLog{}
		if err := rows.Scan(&l.LogID, &l.Action, &l.ResourceID, &l.ResourceName, &l.UserID, &l.CreatedAt, &l.OrganizationID, &l.ProjectID); err != nil {
			return nil, err
		}
		logs = append(logs, l)
	}
	return logs, nil
}

func DeleteAuditLog(ctx context.Context, id string) error {
	query := `DELETE FROM public.audit_logs WHERE log_id = $1`
	_, err := db.GetCTOPoolOrNil().Exec(ctx, query, id)
	return err
}

func CountAuditLogs(ctx context.Context, f AuditFilter) (int64, error) {
	args := []any{}
	where := buildAuditWhere(f, &args)
	query := `SELECT COUNT(*) FROM public.audit_logs al ` + where
	var count int64
	err := db.GetCTOPoolOrNil().QueryRow(ctx, query, args...).Scan(&count)
	return count, err
}

func buildAuditWhere(f AuditFilter, args *[]any) string {
	clauses := []string{}
	if f.ResourceID != "" {
		*args = append(*args, f.ResourceID)
		clauses = append(clauses, "al.resource_id = $"+itoa(len(*args)))
	}
	if f.Action != "" {
		*args = append(*args, f.Action)
		clauses = append(clauses, "al.action = $"+itoa(len(*args)))
	}
	if f.OrganizationID != "" {
		*args = append(*args, f.OrganizationID)
		clauses = append(clauses, "al.organization_id = $"+itoa(len(*args)))
	}
	if f.ProjectID != "" {
		*args = append(*args, f.ProjectID)
		clauses = append(clauses, "al.project_id = $"+itoa(len(*args)))
	}
	if f.Search != "" {
		*args = append(*args, "%"+f.Search+"%")
		clauses = append(clauses, "al.action::text ILIKE $"+itoa(len(*args)))
	}
	if len(clauses) == 0 {
		return ""
	}
	result := "WHERE "
	for i, c := range clauses {
		if i > 0 {
			result += " AND "
		}
		result += c
	}
	return result
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
