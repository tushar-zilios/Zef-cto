package project

import (
	"errors"
	"net/http"
	"strconv"

	dbaudit "cto/src/internal/db/audit"
	dbproject "cto/src/internal/db/project"
	dbvault "cto/src/internal/db/vault"
	auditmodels "cto/src/internal/models/audit"
	models "cto/src/internal/models/project"
	vaultmodels "cto/src/internal/models/vault"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func CreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name           string  `json:"name"`
		OrganizationID *string `json:"organization_id"`
		CreatedBy      *string `json:"created_by"`
	}
	if err := utils.ReadJSON(r, &req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if req.Name == "" {
		utils.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}

	userID, _ := r.Context().Value("user_id").(string)
	createdBy := req.CreatedBy
	if createdBy == nil && userID != "" {
		createdBy = &userID
	}

	p := &models.Project{
		Name:           req.Name,
		OrganizationID: req.OrganizationID,
		CreatedBy:      createdBy,
	}
	if err := dbproject.CreateProject(r.Context(), p); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create project: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, p)
}

func ListProjectsHandler(w http.ResponseWriter, r *http.Request) {
	orgID := r.URL.Query().Get("organization_id")
	list, err := dbproject.ListProjects(r.Context(), orgID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list projects: "+err.Error())
		return
	}
	if list == nil {
		list = []*models.Project{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

func GetProjectHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	p, err := dbproject.GetProject(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Project not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get project: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, p)
}

func DeleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := dbproject.DeleteProject(r.Context(), id); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to delete project: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// --- Project-scoped vault ---

func ListProjectVaultHandler(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	q := r.URL.Query()
	search := q.Get("search")
	limit, offset := parsePagination(q.Get("limit"), q.Get("offset"))

	list, err := dbvault.ListVaultCredentials(r.Context(), "", projectID, search, limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list vault secrets: "+err.Error())
		return
	}
	if list == nil {
		list = []*vaultmodels.VaultCredential{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

// --- Project-scoped audit logs ---

func ListProjectAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	projectID := chi.URLParam(r, "id")
	q := r.URL.Query()
	limit, offset := parsePagination(q.Get("limit"), q.Get("offset"))

	f := dbaudit.AuditFilter{
		ProjectID: projectID,
		SortDesc:  q.Get("sort") != "asc",
	}
	logs, err := dbaudit.ListAuditLogs(r.Context(), f, limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list audit logs: "+err.Error())
		return
	}
	if logs == nil {
		logs = []*auditmodels.AuditLog{}
	}
	utils.WriteJSON(w, http.StatusOK, logs)
}

func parsePagination(limitStr, offsetStr string) (int, int) {
	limit := 20
	offset := 0
	if v, err := strconv.Atoi(limitStr); err == nil && v > 0 {
		limit = v
	}
	if v, err := strconv.Atoi(offsetStr); err == nil && v >= 0 {
		offset = v
	}
	return limit, offset
}
