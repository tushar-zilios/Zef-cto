package audit

import (
	"context"
	"net/http"

	dbaudit "cto/src/internal/db/audit"
	"cto/src/internal/logger"
	models "cto/src/internal/models/audit"
)

// resource IDs seeded in public.resources at startup
const (
	ResourceVault            = "00000005-0000-0000-0000-000000000001"
	ResourceVaultCredentials = "00000005-0000-0000-0000-000000000002"
	ResourceAuditLogs        = "00000005-0000-0000-0000-000000000003"
)

type Event struct {
	Action         string
	ResourceID     string // resource_id FK into public.resources (use constants above)
	OrganizationID string // optional — scopes the log to an org
	ProjectID      string // optional — scopes the log to a project
}

func Log(r *http.Request, ev Event) {
	userID, _ := r.Context().Value("user_id").(string)
	ctxProjectID, _ := r.Context().Value("project_id").(string)

	entry := &models.AuditLog{
		Action: models.ActionType(ev.Action),
	}
	if userID != "" {
		entry.UserID = &userID
	}
	if ev.ResourceID != "" {
		s := ev.ResourceID
		entry.ResourceID = &s
	}
	if ev.OrganizationID != "" {
		s := ev.OrganizationID
		entry.OrganizationID = &s
	}
	projectID := ev.ProjectID
	if projectID == "" {
		projectID = ctxProjectID
	}
	if projectID != "" {
		entry.ProjectID = &projectID
	}

	go func() {
		if err := dbaudit.CreateAuditLog(context.Background(), entry); err != nil {
			logger.LogDB("audit: failed to write log entry (action=%s): %v", ev.Action, err)
		}
	}()
}
