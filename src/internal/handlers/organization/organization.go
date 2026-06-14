package organization

import (
	"errors"
	"net/http"

	dborg "cto/src/internal/db/organization"
	models "cto/src/internal/models/organization"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

// ProvisionOrganizationHandler handles POST /organizations/provision.
// Called by the backend when a workspace installs the CTO extension.
// Creates the organization in the CTO DB if it doesn't already exist.
func ProvisionOrganizationHandler(w http.ResponseWriter, r *http.Request) {
	var org models.Organization
	if err := utils.ReadJSON(r, &org); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if org.OrganizationID == "" || org.OrganizationName == "" {
		utils.WriteError(w, http.StatusBadRequest, "organization_id and organization_name are required")
		return
	}

	if err := dborg.UpsertOrganization(r.Context(), &org); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to provision organization: "+err.Error())
		return
	}

	utils.WriteJSON(w, http.StatusOK, org)
}

func GetOrganizationHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	org, err := dborg.GetOrganizationByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Organization not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get organization: "+err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, org)
}
