package handlers

import (
	"errors"
	"net/http"
	"strconv"

	dbaudit "cto/src/internal/db/audit"
	models "cto/src/internal/models/audit"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// --- Resource Handlers ---

func CreateResourceHandler(w http.ResponseWriter, r *http.Request) {
	var res models.Resource
	if err := utils.ReadJSON(r, &res); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if err := dbaudit.CreateResource(r.Context(), &res); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create resource: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, res)
}

func GetResourceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	res, err := dbaudit.GetResourceByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Resource not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get resource: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, res)
}

func ListResourcesHandler(w http.ResponseWriter, r *http.Request) {
	resources, err := dbaudit.ListResources(r.Context())
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list resources: "+err.Error())
		return
	}
	if resources == nil {
		resources = []*models.Resource{}
	}
	utils.WriteJSON(w, http.StatusOK, resources)
}

func UpdateResourceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var res models.Resource
	if err := utils.ReadJSON(r, &res); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	res.ResourceID = id
	if err := dbaudit.UpdateResource(r.Context(), &res); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to update resource: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, res)
}

func DeleteResourceHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := dbaudit.DeleteResource(r.Context(), id); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to delete resource: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "Resource deleted"})
}

// --- Audit Log Handlers ---

func CreateAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	var log models.AuditLog
	if err := utils.ReadJSON(r, &log); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if err := dbaudit.CreateAuditLog(r.Context(), &log); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create audit log: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, log)
}

func GetAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	log, err := dbaudit.GetAuditLogByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Audit log not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get audit log: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, log)
}

func ListAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	offset, _ := strconv.Atoi(q.Get("offset"))
	if limit <= 0 {
		limit = 50
	}
	projectID, _ := r.Context().Value("project_id").(string)
	if projectID == "" {
		projectID = q.Get("project_id")
	}
	f := dbaudit.AuditFilter{
		ResourceID:     q.Get("resource_id"),
		Action:         q.Get("action"),
		OrganizationID: q.Get("organization_id"),
		ProjectID:      projectID,
		Search:         q.Get("search"),
		SortDesc:       q.Get("sort") != "asc",
	}

	logs, err := dbaudit.ListAuditLogs(r.Context(), f, limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list audit logs: "+err.Error())
		return
	}
	if logs == nil {
		logs = []*models.AuditLog{}
	}
	utils.WriteJSON(w, http.StatusOK, logs)
}

func DeleteAuditLogHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := dbaudit.DeleteAuditLog(r.Context(), id); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to delete audit log: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "Audit log deleted"})
}

func CountAuditLogsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, _ := r.Context().Value("project_id").(string)
	if projectID == "" {
		projectID = q.Get("project_id")
	}
	f := dbaudit.AuditFilter{
		ResourceID:     q.Get("resource_id"),
		Action:         q.Get("action"),
		OrganizationID: q.Get("organization_id"),
		ProjectID:      projectID,
		Search:         q.Get("search"),
	}
	count, err := dbaudit.CountAuditLogs(r.Context(), f)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to count audit logs: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]int64{"count": count})
}
