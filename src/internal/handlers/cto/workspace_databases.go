package cto

import (
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

type WorkspaceDatabaseMapping struct {
	MappingID   string    `json:"mapping_id"`
	WorkspaceID string    `json:"workspace_id"`
	DatabaseID  string    `json:"database_id"`
	GrantedBy   *string   `json:"granted_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

// ListWorkspacesForDatabaseHandler returns all workspaces that have access to a DB project.
// GET /cto/projects/{id}/workspaces
func ListWorkspacesForDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT mapping_id, workspace_id, database_id, granted_by, created_at
		FROM public.workspace_to_database
		WHERE database_id = $1
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []WorkspaceDatabaseMapping
	for rows.Next() {
		var m WorkspaceDatabaseMapping
		if err := rows.Scan(&m.MappingID, &m.WorkspaceID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt); err != nil {
			continue
		}
		list = append(list, m)
	}
	if list == nil {
		list = []WorkspaceDatabaseMapping{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

// ListDatabasesForWorkspaceHandler returns all DB projects a workspace has access to.
// GET /cto/workspace-databases?workspace_id=...
func ListDatabasesForWorkspaceHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT m.mapping_id, m.workspace_id, m.database_id, m.granted_by, m.created_at
		FROM public.workspace_to_database m
		WHERE m.workspace_id = $1
		ORDER BY m.created_at ASC
	`, workspaceID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []WorkspaceDatabaseMapping
	for rows.Next() {
		var m WorkspaceDatabaseMapping
		if err := rows.Scan(&m.MappingID, &m.WorkspaceID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt); err != nil {
			continue
		}
		list = append(list, m)
	}
	if list == nil {
		list = []WorkspaceDatabaseMapping{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

// GrantWorkspaceAccessHandler gives a workspace access to a DB project.
// POST /cto/projects/{id}/workspaces
func GrantWorkspaceAccessHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value("user_id").(string)

	var input struct {
		WorkspaceID string `json:"workspace_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.WorkspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var m WorkspaceDatabaseMapping
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.workspace_to_database (workspace_id, database_id, granted_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid)
		ON CONFLICT (workspace_id, database_id) DO UPDATE SET granted_by = EXCLUDED.granted_by
		RETURNING mapping_id, workspace_id, database_id, granted_by, created_at
	`, input.WorkspaceID, id, userID,
	).Scan(&m.MappingID, &m.WorkspaceID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, m)
}

// RevokeWorkspaceAccessHandler removes a workspace's access to a DB project.
// DELETE /cto/projects/{id}/workspaces/{workspaceId}
func RevokeWorkspaceAccessHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	workspaceID := chi.URLParam(r, "workspaceId")

	tag, err := db.GetCTOPoolOrNil().Exec(r.Context(), `
		DELETE FROM public.workspace_to_database
		WHERE database_id = $1 AND workspace_id = $2
	`, id, workspaceID)
	if err != nil || tag.RowsAffected() == 0 {
		utils.WriteError(w, http.StatusNotFound, "mapping not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "access revoked"})
}
