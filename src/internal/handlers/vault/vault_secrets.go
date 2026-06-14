package vault

import (
	"errors"
	"net/http"
	"strconv"

	"cto/src/internal/audit"
	"cto/src/internal/crypto"
	dbproject "cto/src/internal/db/project"
	dbvault "cto/src/internal/db/vault"
	models "cto/src/internal/models/vault"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
)

func CreateVaultSecretHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if err := utils.ReadJSON(r, &req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if req.Key == "" || req.Value == "" {
		utils.WriteError(w, http.StatusBadRequest, "key and value are required")
		return
	}

	encVal, encDEK, err := crypto.EncryptSecret(req.Value)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Encryption failed: "+err.Error())
		return
	}

	userID, _ := r.Context().Value("user_id").(string)
	projectID, _ := r.Context().Value("project_id").(string)

	c := &models.VaultCredential{
		Key:            req.Key,
		EncryptedValue: encVal,
		EncryptedDEK:   encDEK,
	}
	if userID != "" {
		c.CreatedBy = &userID
	}
	if projectID != "" {
		c.ProjectID = &projectID
		// derive org from the project so we don't rely on X-Workspace-ID
		// (which carries the main-backend workspace UUID, not a CTO org UUID)
		if p, err := dbproject.GetProject(r.Context(), projectID); err == nil && p.OrganizationID != nil {
			c.OrganizationID = p.OrganizationID
		}
	}
	if err := dbvault.CreateVaultCredential(r.Context(), c); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to create vault secret: "+err.Error())
		return
	}

	audit.Log(r, audit.Event{
		Action:         "create",
		ResourceID:     audit.ResourceVaultCredentials,
		OrganizationID: strVal(c.OrganizationID),
	})
	utils.WriteJSON(w, http.StatusCreated, c)
}

func GetVaultSecretHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := dbvault.GetVaultCredentialByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Vault secret not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get vault secret: "+err.Error())
		return
	}

	plaintext, err := crypto.DecryptSecret(c.EncryptedValue, c.EncryptedDEK)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Decryption failed: "+err.Error())
		return
	}

	audit.Log(r, audit.Event{
		Action:         "read",
		ResourceID:     audit.ResourceVaultCredentials,
		OrganizationID: strVal(c.OrganizationID),
	})
	c.Value = plaintext
	utils.WriteJSON(w, http.StatusOK, c)
}

func UpdateVaultSecretHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	var req struct {
		Value string `json:"value"`
	}
	if err := utils.ReadJSON(r, &req); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "Invalid request payload: "+err.Error())
		return
	}
	if req.Value == "" {
		utils.WriteError(w, http.StatusBadRequest, "value is required")
		return
	}

	c, err := dbvault.GetVaultCredentialByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Vault secret not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to get vault secret: "+err.Error())
		return
	}

	encVal, encDEK, err := crypto.EncryptSecret(req.Value)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Encryption failed: "+err.Error())
		return
	}

	c.EncryptedValue = encVal
	c.EncryptedDEK = encDEK
	if err := dbvault.UpdateVaultCredential(r.Context(), c); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to update vault secret: "+err.Error())
		return
	}

	audit.Log(r, audit.Event{
		Action:         "update",
		ResourceID:     audit.ResourceVaultCredentials,
		OrganizationID: strVal(c.OrganizationID),
	})
	utils.WriteJSON(w, http.StatusOK, c)
}

func DeleteVaultSecretHandler(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	c, err := dbvault.GetVaultCredentialByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			utils.WriteError(w, http.StatusNotFound, "Vault secret not found")
			return
		}
		utils.WriteError(w, http.StatusInternalServerError, "Failed to check vault secret: "+err.Error())
		return
	}

	if err := dbvault.DeleteVaultCredential(r.Context(), id); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to delete vault secret: "+err.Error())
		return
	}

	audit.Log(r, audit.Event{
		Action:         "delete",
		ResourceID:     audit.ResourceVaultCredentials,
		OrganizationID: strVal(c.OrganizationID),
	})
	utils.WriteJSON(w, http.StatusNoContent, nil)
}

func ListVaultSecretsHandler(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	projectID, _ := r.Context().Value("project_id").(string)
	if projectID == "" {
		projectID = q.Get("project_id")
	}
	// derive org from the project so the filter uses the CTO org UUID,
	// not the main-backend workspace UUID that X-Workspace-ID carries
	orgID := ""
	if projectID != "" {
		if p, err := dbproject.GetProject(r.Context(), projectID); err == nil && p.OrganizationID != nil {
			orgID = *p.OrganizationID
		}
	}
	search := q.Get("search")
	limit, offset := parsePagination(q.Get("limit"), q.Get("offset"))

	list, err := dbvault.ListVaultCredentials(r.Context(), orgID, projectID, search, limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to list vault secrets: "+err.Error())
		return
	}
	if list == nil {
		list = []*models.VaultCredential{}
	}

	audit.Log(r, audit.Event{
		Action:         "read",
		ResourceID:     audit.ResourceVaultCredentials,
		OrganizationID: orgID, // derived from project, may be empty string
	})
	utils.WriteJSON(w, http.StatusOK, list)
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

func strVal(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
