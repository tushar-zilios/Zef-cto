package cto

import (
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

type SavedQuery struct {
	QueryID     string    `json:"query_id"`
	DatabaseID  string    `json:"database_id"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	SQLText     string    `json:"sql_text"`
	IsShared    bool      `json:"is_shared"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type SavedQueryInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SQLText     string `json:"sql_text"`
	IsShared    bool   `json:"is_shared"`
}

func ListSavedQueriesHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value("user_id").(string)

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT query_id, database_id, created_by, name, description, sql_text, is_shared, created_at, updated_at
		FROM public.cto_saved_queries
		WHERE database_id = $1
		  AND (is_shared = true OR created_by = NULLIF($2, '')::uuid)
		ORDER BY updated_at DESC
	`, id, userID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []SavedQuery
	for rows.Next() {
		var q SavedQuery
		if err := rows.Scan(&q.QueryID, &q.DatabaseID, &q.CreatedBy, &q.Name, &q.Description,
			&q.SQLText, &q.IsShared, &q.CreatedAt, &q.UpdatedAt); err != nil {
			continue
		}
		list = append(list, q)
	}
	if list == nil {
		list = []SavedQuery{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

func CreateSavedQueryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value("user_id").(string)

	var input SavedQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" || input.SQLText == "" {
		utils.WriteError(w, http.StatusBadRequest, "name and sql_text are required")
		return
	}

	var q SavedQuery
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.cto_saved_queries (database_id, created_by, name, description, sql_text, is_shared)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6)
		RETURNING query_id, database_id, created_by, name, description, sql_text, is_shared, created_at, updated_at
	`, id, userID, input.Name, input.Description, input.SQLText, input.IsShared,
	).Scan(&q.QueryID, &q.DatabaseID, &q.CreatedBy, &q.Name, &q.Description,
		&q.SQLText, &q.IsShared, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, q)
}

func UpdateSavedQueryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	queryID := chi.URLParam(r, "queryId")
	userID, _ := r.Context().Value("user_id").(string)

	var input SavedQueryInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" || input.SQLText == "" {
		utils.WriteError(w, http.StatusBadRequest, "name and sql_text are required")
		return
	}

	var q SavedQuery
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		UPDATE public.cto_saved_queries
		SET name=$1, description=$2, sql_text=$3, is_shared=$4, updated_at=NOW()
		WHERE query_id=$5 AND database_id=$6 AND (created_by = NULLIF($7,'')::uuid OR is_shared = true)
		RETURNING query_id, database_id, created_by, name, description, sql_text, is_shared, created_at, updated_at
	`, input.Name, input.Description, input.SQLText, input.IsShared, queryID, id, userID,
	).Scan(&q.QueryID, &q.DatabaseID, &q.CreatedBy, &q.Name, &q.Description,
		&q.SQLText, &q.IsShared, &q.CreatedAt, &q.UpdatedAt)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "query not found or not authorized")
		return
	}
	utils.WriteJSON(w, http.StatusOK, q)
}

func DeleteSavedQueryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	queryID := chi.URLParam(r, "queryId")
	userID, _ := r.Context().Value("user_id").(string)

	tag, err := db.GetCTOPoolOrNil().Exec(r.Context(), `
		DELETE FROM public.cto_saved_queries
		WHERE query_id = $1 AND database_id = $2 AND created_by = NULLIF($3,'')::uuid
	`, queryID, id, userID)
	if err != nil || tag.RowsAffected() == 0 {
		utils.WriteError(w, http.StatusNotFound, "query not found or not authorized")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}
