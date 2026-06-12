package cto

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type SchemaSnapshot struct {
	SnapshotID string          `json:"snapshot_id"`
	DatabaseID string          `json:"database_id"`
	CapturedBy *string         `json:"captured_by,omitempty"`
	Label      string          `json:"label"`
	SchemaJSON json.RawMessage `json:"schema_json"`
	CapturedAt time.Time       `json:"captured_at"`
}

type SchemaSnapshotSummary struct {
	SnapshotID string    `json:"snapshot_id"`
	DatabaseID string    `json:"database_id"`
	Label      string    `json:"label"`
	CapturedAt time.Time `json:"captured_at"`
}

func CaptureSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	userID, _ := r.Context().Value("user_id").(string)

	var input struct {
		Label string `json:"label"`
	}
	json.NewDecoder(r.Body).Decode(&input)

	pool := db.GetCTOPoolOrNil()
	schemaJSON, err := captureSchemaJSON(r.Context(), pool)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "schema capture failed: "+err.Error())
		return
	}

	raw, _ := json.Marshal(schemaJSON)

	var snap SchemaSnapshot
	err = pool.QueryRow(r.Context(), `
		INSERT INTO public.cto_schema_snapshots (database_id, captured_by, label, schema_json)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4)
		RETURNING snapshot_id, database_id, captured_by, label, schema_json, captured_at
	`, id, userID, input.Label, string(raw),
	).Scan(&snap.SnapshotID, &snap.DatabaseID, &snap.CapturedBy, &snap.Label, &snap.SchemaJSON, &snap.CapturedAt)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, snap)
}

func ListSnapshotsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT snapshot_id, database_id, label, captured_at
		FROM public.cto_schema_snapshots
		WHERE database_id = $1
		ORDER BY captured_at DESC
	`, id)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var list []SchemaSnapshotSummary
	for rows.Next() {
		var s SchemaSnapshotSummary
		if err := rows.Scan(&s.SnapshotID, &s.DatabaseID, &s.Label, &s.CapturedAt); err != nil {
			continue
		}
		list = append(list, s)
	}
	if list == nil {
		list = []SchemaSnapshotSummary{}
	}
	utils.WriteJSON(w, http.StatusOK, list)
}

func GetSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	snapshotID := chi.URLParam(r, "snapshotId")

	var snap SchemaSnapshot
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		SELECT snapshot_id, database_id, captured_by, label, schema_json, captured_at
		FROM public.cto_schema_snapshots
		WHERE snapshot_id = $1 AND database_id = $2
	`, snapshotID, id).Scan(&snap.SnapshotID, &snap.DatabaseID, &snap.CapturedBy,
		&snap.Label, &snap.SchemaJSON, &snap.CapturedAt)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, snap)
}

func DeleteSnapshotHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	snapshotID := chi.URLParam(r, "snapshotId")

	tag, err := db.GetCTOPoolOrNil().Exec(r.Context(), `
		DELETE FROM public.cto_schema_snapshots WHERE snapshot_id = $1 AND database_id = $2
	`, snapshotID, id)
	if err != nil || tag.RowsAffected() == 0 {
		utils.WriteError(w, http.StatusNotFound, "snapshot not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func captureSchemaJSON(ctx context.Context, pool *pgxpool.Pool) (map[string]any, error) {
	result := map[string]any{}

	tableRows, err := pool.Query(ctx, `
		SELECT c.table_schema, c.table_name, c.column_name, c.data_type,
		       c.is_nullable, c.column_default, c.character_maximum_length
		FROM information_schema.columns c
		JOIN information_schema.tables t
		  ON t.table_schema = c.table_schema AND t.table_name = c.table_name
		WHERE c.table_schema NOT IN ('pg_catalog','information_schema','pg_toast')
		  AND t.table_type = 'BASE TABLE'
		ORDER BY c.table_schema, c.table_name, c.ordinal_position
	`)
	if err != nil {
		return nil, err
	}
	defer tableRows.Close()

	tables := map[string]map[string]any{}
	for tableRows.Next() {
		var schema, table, col, dtype, nullable string
		var def, maxLen *string
		if err := tableRows.Scan(&schema, &table, &col, &dtype, &nullable, &def, &maxLen); err != nil {
			continue
		}
		key := schema + "." + table
		if _, ok := tables[key]; !ok {
			tables[key] = map[string]any{"schema": schema, "name": table, "columns": []any{}}
		}
		colInfo := map[string]any{"name": col, "type": dtype, "nullable": nullable == "YES"}
		if def != nil {
			colInfo["default"] = *def
		}
		tables[key]["columns"] = append(tables[key]["columns"].([]any), colInfo)
	}
	result["tables"] = tables

	viewRows, err := pool.Query(ctx, `
		SELECT table_schema, table_name, view_definition
		FROM information_schema.views
		WHERE table_schema NOT IN ('pg_catalog','information_schema')
	`)
	if err == nil {
		defer viewRows.Close()
		var views []any
		for viewRows.Next() {
			var schema, name string
			var def *string
			if err := viewRows.Scan(&schema, &name, &def); err != nil {
				continue
			}
			v := map[string]any{"schema": schema, "name": name}
			if def != nil {
				v["definition"] = *def
			}
			views = append(views, v)
		}
		result["views"] = views
	}

	idxRows, err := pool.Query(ctx, `
		SELECT schemaname, tablename, indexname, indexdef
		FROM pg_indexes
		WHERE schemaname NOT IN ('pg_catalog','information_schema')
		ORDER BY schemaname, tablename, indexname
	`)
	if err == nil {
		defer idxRows.Close()
		var indexes []any
		for idxRows.Next() {
			var schema, table, name, def string
			if err := idxRows.Scan(&schema, &table, &name, &def); err != nil {
				continue
			}
			indexes = append(indexes, map[string]any{"schema": schema, "table": table, "name": name, "definition": def})
		}
		result["indexes"] = indexes
	}

	return result, nil
}
