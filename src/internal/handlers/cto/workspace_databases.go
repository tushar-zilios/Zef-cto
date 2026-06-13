package cto

import (
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

type OrganizationDatabaseMapping struct {
	MappingID      string    `json:"mapping_id"`
	OrganizationID string    `json:"organization_id"`
	DatabaseID     string    `json:"database_id"`
	GrantedBy      *string   `json:"granted_by,omitempty"`
	CreatedAt      time.Time `json:"created_at"`
}

// ListOrganizationsForDatabaseHandler returns all organizations that have access to a DB project.
// GET /cto/projects/{id}/organizations
func ListOrganizationsForDatabaseHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT mapping_id, organization_id, database_id, granted_by, created_at
		FROM public.organization_to_database
		WHERE database_id = $1
		ORDER BY created_at ASC
	`, id)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []OrganizationDatabaseMapping
	for rows.Next() {
		var m OrganizationDatabaseMapping
		if err := rows.Scan(&m.MappingID, &m.OrganizationID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt); err != nil {
			continue
		}
		list = append(list, m)
	}
	if list == nil {
		list = []OrganizationDatabaseMapping{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

// ListDatabasesForOrganizationHandler returns all DB projects an organization has access to.
// GET /cto/organization-databases?organization_id=...
func ListDatabasesForOrganizationHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	organizationID := r.URL.Query().Get("organization_id")
	if organizationID == "" {
		utils.WriteError(w, http.StatusBadRequest, "organization_id is required")
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT m.mapping_id, m.organization_id, m.database_id, m.granted_by, m.created_at
		FROM public.organization_to_database m
		WHERE m.organization_id = $1
		ORDER BY m.created_at ASC
	`, organizationID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []OrganizationDatabaseMapping
	for rows.Next() {
		var m OrganizationDatabaseMapping
		if err := rows.Scan(&m.MappingID, &m.OrganizationID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt); err != nil {
			continue
		}
		list = append(list, m)
	}
	if list == nil {
		list = []OrganizationDatabaseMapping{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

// GrantOrganizationAccessHandler gives an organization access to a DB project.
// POST /cto/projects/{id}/organizations
func GrantOrganizationAccessHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value("user_id").(string)

	var input struct {
		OrganizationID string `json:"organization_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.OrganizationID == "" {
		utils.WriteError(w, http.StatusBadRequest, "organization_id is required")
		return
	}

	var m OrganizationDatabaseMapping
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.organization_to_database (organization_id, database_id, granted_by)
		VALUES ($1, $2, NULLIF($3, '')::uuid)
		ON CONFLICT (organization_id, database_id) DO UPDATE SET granted_by = EXCLUDED.granted_by
		RETURNING mapping_id, organization_id, database_id, granted_by, created_at
	`, input.OrganizationID, id, userID,
	).Scan(&m.MappingID, &m.OrganizationID, &m.DatabaseID, &m.GrantedBy, &m.CreatedAt)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, m)
}

// RevokeOrganizationAccessHandler removes an organization's access to a DB project.
// DELETE /cto/projects/{id}/organizations/{organizationId}
func RevokeOrganizationAccessHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	organizationID := chi.URLParam(r, "organizationId")

	tag, err := db.GetCTOPoolOrNil().Exec(r.Context(), `
		DELETE FROM public.organization_to_database
		WHERE database_id = $1 AND organization_id = $2
	`, id, organizationID)
	if err != nil || tag.RowsAffected() == 0 {
		utils.WriteError(w, http.StatusNotFound, "mapping not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "access revoked"})
}
