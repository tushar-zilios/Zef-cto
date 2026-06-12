package cto

import (
	"net/http"
	"strconv"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

type SQLHistoryEntry struct {
	HistoryID    string    `json:"history_id"`
	DatabaseID   string    `json:"database_id"`
	UserID       *string   `json:"user_id,omitempty"`
	SQLText      string    `json:"sql_text"`
	ExecutionMS  *int      `json:"execution_ms,omitempty"`
	RowCount     *int      `json:"row_count,omitempty"`
	HadError     bool      `json:"had_error"`
	ErrorMessage string    `json:"error_message"`
	ExecutedAt   time.Time `json:"executed_at"`
}

func recordSQLHistory(r *http.Request, databaseID, sqlText string, execMS, rowCount int, hadError bool, errMsg string) {
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return
	}
	userID, _ := r.Context().Value("user_id").(string)
	pool.Exec(r.Context(), `
		INSERT INTO public.cto_sql_history
			(database_id, user_id, sql_text, execution_ms, row_count, had_error, error_message)
		VALUES ($1, NULLIF($2,''), $3, $4, $5, $6, $7)
	`, databaseID, userID, sqlText, execMS, rowCount, hadError, errMsg)
}

func ListSQLHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	limit := 50
	offset := 0
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 200 {
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT history_id, database_id, user_id, sql_text, execution_ms, row_count, had_error, error_message, executed_at
		FROM public.cto_sql_history
		WHERE database_id = $1
		ORDER BY executed_at DESC
		LIMIT $2 OFFSET $3
	`, id, limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []SQLHistoryEntry
	for rows.Next() {
		var e SQLHistoryEntry
		if err := rows.Scan(&e.HistoryID, &e.DatabaseID, &e.UserID, &e.SQLText,
			&e.ExecutionMS, &e.RowCount, &e.HadError, &e.ErrorMessage, &e.ExecutedAt); err != nil {
			continue
		}
		list = append(list, e)
	}
	if list == nil {
		list = []SQLHistoryEntry{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

func ClearSQLHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	db.GetCTOPoolOrNil().Exec(r.Context(), `DELETE FROM public.cto_sql_history WHERE database_id = $1`, id)
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "history cleared"})
}
