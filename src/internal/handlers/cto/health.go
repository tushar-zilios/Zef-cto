package cto

import (
	"context"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type HealthRecord struct {
	HealthID     string `json:"health_id"`
	DatabaseID   string `json:"database_id"`
	CheckedAt    string `json:"checked_at"`
	OK           bool   `json:"ok"`
	LatencyMS    *int   `json:"latency_ms"`
	ErrorMessage string `json:"error_message"`
}

type TestResult struct {
	OK        bool   `json:"ok"`
	LatencyMS int    `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

func TestConnectionHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	pool := db.GetCTOPoolOrNil()

	var host string
	var port int
	var dbName, username, dbType string
	err := pool.QueryRow(r.Context(), `
		SELECT host, port, db_name, username, db_type
		FROM public.cto_database_projects
		WHERE database_id = $1
	`, id).Scan(&host, &port, &dbName, &username, &dbType)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "project not found")
		return
	}

	var passEnc string
	pool.QueryRow(r.Context(), `
		SELECT password_enc FROM public.cto_database_credentials WHERE database_id = $1
	`, id).Scan(&passEnc)

	password, _ := decrypt(passEnc)

	start := time.Now()
	testErr := pingPostgres(host, port, dbName, username, password)
	latencyMS := int(time.Since(start).Milliseconds())

	ok := testErr == nil
	errMsg := ""
	if testErr != nil {
		errMsg = testErr.Error()
	}

	pool.Exec(r.Context(), `
		INSERT INTO public.cto_connection_health (database_id, ok, latency_ms, error_message)
		VALUES ($1, $2, $3, $4)
	`, id, ok, latencyMS, errMsg)

	if ok {
		pool.Exec(r.Context(), `
			UPDATE public.cto_database_projects SET last_connected_at = NOW() WHERE database_id = $1
		`, id)
	}

	pool.Exec(r.Context(), `
		DELETE FROM public.cto_connection_health
		WHERE database_id = $1
		  AND health_id NOT IN (
		    SELECT health_id FROM public.cto_connection_health
		    WHERE database_id = $1
		    ORDER BY checked_at DESC
		    LIMIT 100
		  )
	`, id)

	result := TestResult{OK: ok, LatencyMS: latencyMS}
	if !ok {
		result.Error = errMsg
	}
	utils.WriteJSON(w, http.StatusOK, result)
}

func GetHealthHistoryHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	limit := 50

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT health_id, database_id, checked_at, ok, latency_ms, error_message
		FROM public.cto_connection_health
		WHERE database_id = $1
		ORDER BY checked_at DESC
		LIMIT $2
	`, id, limit)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var records []HealthRecord
	for rows.Next() {
		var h HealthRecord
		if err := rows.Scan(&h.HealthID, &h.DatabaseID, &h.CheckedAt, &h.OK, &h.LatencyMS, &h.ErrorMessage); err != nil {
			continue
		}
		records = append(records, h)
	}
	if records == nil {
		records = []HealthRecord{}
	}
	utils.WriteJSON(w, http.StatusOK, records)
}

func pingPostgres(host string, port int, dbName, username, password string) error {
	if host == "" {
		return nil
	}
	dsn := buildDSN(host, port, dbName, username, password)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return err
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 1

	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return err
	}
	defer p.Close()
	return p.Ping(ctx)
}

func buildDSN(host string, port int, dbName, username, password string) string {
	if port == 0 {
		port = 5432
	}
	if dbName == "" {
		dbName = "postgres"
	}
	return "postgres://" + username + ":" + password + "@" + host + ":" + itoa(port) + "/" + dbName + "?sslmode=prefer"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
