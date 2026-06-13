package cto

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

var timeNow = time.Now
var timeSince = time.Since

func ctoDBRequired(w http.ResponseWriter) bool {
	if db.GetCTOPoolOrNil() == nil {
		utils.WriteError(w, http.StatusServiceUnavailable, "database is not configured (DATABASE_URL not set)")
		return false
	}
	return true
}

type TableInfo struct {
	Schema string `json:"schema"`
	Name   string `json:"name"`
	Type   string `json:"type"`
}

type ColumnInfo struct {
	Name      string  `json:"name"`
	Type      string  `json:"type"`
	Nullable  bool    `json:"nullable"`
	Default   *string `json:"default,omitempty"`
	IsPrimary bool    `json:"is_primary"`
	IsUnique  bool    `json:"is_unique"`
	MaxLength *int    `json:"max_length,omitempty"`
	Comment   *string `json:"comment,omitempty"`
}

type RowsResult struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
	Total   int64    `json:"total"`
	Limit   int      `json:"limit"`
	Offset  int      `json:"offset"`
}

type SQLResult struct {
	Columns  []string `json:"columns"`
	Rows     [][]any  `json:"rows"`
	RowCount int      `json:"row_count"`
	Command  string   `json:"command,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type ExtensionInfo struct {
	Name             string  `json:"name"`
	InstalledVersion string  `json:"installed_version"`
	Schema           string  `json:"schema"`
	Comment          *string `json:"comment,omitempty"`
}

type FunctionInfo struct {
	Schema     string  `json:"schema"`
	Name       string  `json:"name"`
	ReturnType string  `json:"return_type"`
	Arguments  string  `json:"arguments"`
	Language   string  `json:"language"`
	Definition *string `json:"definition,omitempty"`
	Comment    *string `json:"comment,omitempty"`
}

type TriggerInfo struct {
	Schema      string `json:"schema"`
	Name        string `json:"name"`
	TableSchema string `json:"table_schema"`
	Table       string `json:"table"`
	Timing      string `json:"timing"`
	Events      string `json:"events"`
	Orientation string `json:"orientation"`
	Statement   string `json:"statement"`
}

type RoleInfo struct {
	Name          string   `json:"name"`
	IsSuperuser   bool     `json:"is_superuser"`
	CanInherit    bool     `json:"can_inherit"`
	CanCreateRole bool     `json:"can_create_role"`
	CanCreateDB   bool     `json:"can_create_db"`
	CanLogin      bool     `json:"can_login"`
	IsReplication bool     `json:"is_replication"`
	ConnLimit     int      `json:"conn_limit"`
	BypassRLS     bool     `json:"bypass_rls"`
	MemberOf      []string `json:"member_of"`
}

type ViewInfo struct {
	Schema     string  `json:"schema"`
	Name       string  `json:"name"`
	Definition *string `json:"definition,omitempty"`
}

func ListSchemasHandler(w http.ResponseWriter, r *http.Request) {
	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT schema_name
		FROM information_schema.schemata
		WHERE schema_name NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND schema_name NOT LIKE 'pg_temp_%'
		  AND schema_name NOT LIKE 'pg_toast_temp_%'
		ORDER BY schema_name
	`)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var schemas []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			continue
		}
		schemas = append(schemas, name)
	}
	if schemas == nil {
		schemas = []string{}
	}
	utils.WriteJSON(w, http.StatusOK, schemas)
}

func ListTablesHandler(w http.ResponseWriter, r *http.Request) {
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}

	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT table_schema, table_name, table_type
		FROM information_schema.tables
		WHERE table_schema = $1
		ORDER BY table_type, table_name
	`, schema)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var tables []TableInfo
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.Type); err != nil {
			continue
		}
		tables = append(tables, t)
	}
	if tables == nil {
		tables = []TableInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, tables)
}

func ListColumnsHandler(w http.ResponseWriter, r *http.Request) {
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")

	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT
			c.column_name,
			c.data_type,
			c.is_nullable = 'YES' AS nullable,
			c.column_default,
			c.character_maximum_length,
			EXISTS (
				SELECT 1 FROM information_schema.table_constraints tc
				JOIN information_schema.constraint_column_usage ccu
					ON tc.constraint_name = ccu.constraint_name
					AND tc.table_schema = ccu.table_schema
				WHERE tc.constraint_type = 'PRIMARY KEY'
				  AND tc.table_schema = c.table_schema
				  AND tc.table_name = c.table_name
				  AND ccu.column_name = c.column_name
			) AS is_primary,
			EXISTS (
				SELECT 1 FROM information_schema.table_constraints tc
				JOIN information_schema.constraint_column_usage ccu
					ON tc.constraint_name = ccu.constraint_name
					AND tc.table_schema = ccu.table_schema
				WHERE tc.constraint_type = 'UNIQUE'
				  AND tc.table_schema = c.table_schema
				  AND tc.table_name = c.table_name
				  AND ccu.column_name = c.column_name
			) AS is_unique,
			pgd.description
		FROM information_schema.columns c
		LEFT JOIN pg_catalog.pg_statio_all_tables st
			ON st.schemaname = c.table_schema AND st.relname = c.table_name
		LEFT JOIN pg_catalog.pg_description pgd
			ON pgd.objoid = st.relid AND pgd.objsubid = c.ordinal_position
		WHERE c.table_schema = $1 AND c.table_name = $2
		ORDER BY c.ordinal_position
	`, schema, table)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var columns []ColumnInfo
	for rows.Next() {
		var col ColumnInfo
		if err := rows.Scan(
			&col.Name, &col.Type, &col.Nullable, &col.Default,
			&col.MaxLength, &col.IsPrimary, &col.IsUnique, &col.Comment,
		); err != nil {
			continue
		}
		columns = append(columns, col)
	}
	if columns == nil {
		columns = []ColumnInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, columns)
}

func GetTableRowsHandler(w http.ResponseWriter, r *http.Request) {
	schema := chi.URLParam(r, "schema")
	table := chi.URLParam(r, "table")
	if !isValidIdentifier(schema) || !isValidIdentifier(table) {
		utils.WriteError(w, http.StatusBadRequest, "invalid schema or table name")
		return
	}
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l > 0 && l <= 1000 {
		limit = l
	}
	if o, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && o >= 0 {
		offset = o
	}

	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	var total int64
	_ = q.QueryRow(r.Context(), fmt.Sprintf(
		`SELECT COUNT(*) FROM %s.%s`, quoteIdent(schema), quoteIdent(table),
	)).Scan(&total)

	rows, err := q.Query(r.Context(), fmt.Sprintf(
		`SELECT * FROM %s.%s LIMIT $1 OFFSET $2`, quoteIdent(schema), quoteIdent(table),
	), limit, offset)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var resultRows [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		safe := make([]any, len(vals))
		for j, v := range vals {
			safe[j] = toJSONSafe(v)
		}
		resultRows = append(resultRows, safe)
	}
	if resultRows == nil {
		resultRows = [][]any{}
	}
	utils.WriteJSON(w, http.StatusOK, RowsResult{
		Columns: columns, Rows: resultRows, Total: total, Limit: limit, Offset: offset,
	})
}

func ExecuteSQLHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Query      string `json:"query"`
		DatabaseID string `json:"database_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Query == "" {
		utils.WriteError(w, http.StatusBadRequest, "query is required")
		return
	}

	q, cleanup, ok := projectQuerier(w, r, req.DatabaseID)
	if !ok {
		return
	}
	defer cleanup()

	start := timeNow()
	rows, err := q.Query(r.Context(), req.Query)
	execMS := int(timeSince(start).Milliseconds())

	if err != nil {
		recordSQLHistory(r, req.DatabaseID, req.Query, execMS, 0, true, err.Error())
		utils.WriteJSON(w, http.StatusOK, SQLResult{Error: err.Error()})
		return
	}
	defer rows.Close()

	fds := rows.FieldDescriptions()
	columns := make([]string, len(fds))
	for i, fd := range fds {
		columns[i] = string(fd.Name)
	}

	var resultRows [][]any
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			continue
		}
		safe := make([]any, len(vals))
		for j, v := range vals {
			safe[j] = toJSONSafe(v)
		}
		resultRows = append(resultRows, safe)
	}
	if resultRows == nil {
		resultRows = [][]any{}
	}

	if rows.Err() != nil {
		recordSQLHistory(r, req.DatabaseID, req.Query, execMS, 0, true, rows.Err().Error())
		utils.WriteJSON(w, http.StatusOK, SQLResult{Error: rows.Err().Error()})
		return
	}

	recordSQLHistory(r, req.DatabaseID, req.Query, execMS, len(resultRows), false, "")

	cmdTag := rows.CommandTag()
	utils.WriteJSON(w, http.StatusOK, SQLResult{
		Columns: columns, Rows: resultRows, RowCount: len(resultRows), Command: cmdTag.String(),
	})
}

func ListExtensionsHandler(w http.ResponseWriter, r *http.Request) {
	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT e.extname, e.extversion, n.nspname, c.comment
		FROM pg_extension e
		LEFT JOIN pg_namespace n ON n.oid = e.extnamespace
		LEFT JOIN (SELECT name, comment FROM pg_available_extensions) c ON c.name = e.extname
		ORDER BY e.extname
	`)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var exts []ExtensionInfo
	for rows.Next() {
		var e ExtensionInfo
		if err := rows.Scan(&e.Name, &e.InstalledVersion, &e.Schema, &e.Comment); err != nil {
			continue
		}
		exts = append(exts, e)
	}
	if exts == nil {
		exts = []ExtensionInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, exts)
}

func ListFunctionsHandler(w http.ResponseWriter, r *http.Request) {
	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT n.nspname, p.proname, pg_get_function_result(p.oid),
		       pg_get_function_arguments(p.oid), l.lanname, p.prosrc, obj_description(p.oid, 'pg_proc')
		FROM pg_proc p
		LEFT JOIN pg_namespace n ON n.oid = p.pronamespace
		LEFT JOIN pg_language l ON l.oid = p.prolang
		WHERE n.nspname NOT IN ('pg_catalog', 'information_schema', 'pg_toast')
		  AND p.prokind = 'f'
		ORDER BY n.nspname, p.proname
	`)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var funcs []FunctionInfo
	for rows.Next() {
		var f FunctionInfo
		if err := rows.Scan(&f.Schema, &f.Name, &f.ReturnType, &f.Arguments, &f.Language, &f.Definition, &f.Comment); err != nil {
			continue
		}
		funcs = append(funcs, f)
	}
	if funcs == nil {
		funcs = []FunctionInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, funcs)
}

func ListTriggersHandler(w http.ResponseWriter, r *http.Request) {
	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT trigger_schema, trigger_name, event_object_schema, event_object_table,
		       action_timing, string_agg(event_manipulation, ', ' ORDER BY event_manipulation),
		       action_orientation, action_statement
		FROM information_schema.triggers
		WHERE trigger_schema NOT IN ('pg_catalog', 'information_schema')
		GROUP BY trigger_schema, trigger_name, event_object_schema, event_object_table,
		         action_timing, action_orientation, action_statement
		ORDER BY trigger_schema, event_object_table, trigger_name
	`)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var triggers []TriggerInfo
	for rows.Next() {
		var t TriggerInfo
		if err := rows.Scan(&t.Schema, &t.Name, &t.TableSchema, &t.Table, &t.Timing, &t.Events, &t.Orientation, &t.Statement); err != nil {
			continue
		}
		triggers = append(triggers, t)
	}
	if triggers == nil {
		triggers = []TriggerInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, triggers)
}

func ListRolesHandler(w http.ResponseWriter, r *http.Request) {
	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT r.rolname, r.rolsuper, r.rolinherit, r.rolcreaterole, r.rolcreatedb,
		       r.rolcanlogin, r.rolreplication, r.rolconnlimit, r.rolbypassrls,
		       COALESCE(array_agg(m.rolname) FILTER (WHERE m.rolname IS NOT NULL), '{}')
		FROM pg_roles r
		LEFT JOIN pg_auth_members am ON am.member = r.oid
		LEFT JOIN pg_roles m ON m.oid = am.roleid
		WHERE r.rolname NOT LIKE 'pg_%'
		GROUP BY r.rolname, r.rolsuper, r.rolinherit, r.rolcreaterole, r.rolcreatedb,
		         r.rolcanlogin, r.rolreplication, r.rolconnlimit, r.rolbypassrls
		ORDER BY r.rolname
	`)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var roles []RoleInfo
	for rows.Next() {
		var role RoleInfo
		if err := rows.Scan(
			&role.Name, &role.IsSuperuser, &role.CanInherit, &role.CanCreateRole,
			&role.CanCreateDB, &role.CanLogin, &role.IsReplication,
			&role.ConnLimit, &role.BypassRLS, &role.MemberOf,
		); err != nil {
			continue
		}
		roles = append(roles, role)
	}
	if roles == nil {
		roles = []RoleInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, roles)
}

func ListViewsHandler(w http.ResponseWriter, r *http.Request) {
	schema := r.URL.Query().Get("schema")
	if schema == "" {
		schema = "public"
	}

	q, cleanup, ok := projectQuerier(w, r, r.URL.Query().Get("database_id"))
	if !ok {
		return
	}
	defer cleanup()

	rows, err := q.Query(r.Context(), `
		SELECT table_schema, table_name, view_definition
		FROM information_schema.views
		WHERE table_schema = $1
		ORDER BY table_name
	`, schema)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var views []ViewInfo
	for rows.Next() {
		var v ViewInfo
		if err := rows.Scan(&v.Schema, &v.Name, &v.Definition); err != nil {
			continue
		}
		views = append(views, v)
	}
	if views == nil {
		views = []ViewInfo{}
	}
	utils.WriteJSON(w, http.StatusOK, views)
}

func isValidIdentifier(s string) bool {
	if len(s) == 0 || len(s) > 63 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func quoteIdent(s string) string {
	return `"` + s + `"`
}

func toJSONSafe(v any) any {
	if v == nil {
		return nil
	}
	switch val := v.(type) {
	case int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64, bool:
		return val
	case string:
		return val
	case []byte:
		return string(val)
	default:
		return fmt.Sprintf("%v", val)
	}
}
