package cto

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"cto/src/internal/db"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// dbQuerier is satisfied by both *pgxpool.Pool and *pgx.Conn.
type dbQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// openProjectConn decrypts the project's credentials and opens a direct pgx connection
// to the external database. Caller must call conn.Close(ctx) when done.
func openProjectConn(ctx context.Context, databaseID string) (*pgx.Conn, error) {
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return nil, fmt.Errorf("CTO database not configured")
	}

	var host string
	var port int
	var dbName, username string
	err := pool.QueryRow(ctx, `
		SELECT host, port, db_name, username
		FROM public.cto_database_projects
		WHERE database_id = $1
	`, databaseID).Scan(&host, &port, &dbName, &username)
	if err != nil {
		return nil, fmt.Errorf("project not found")
	}
	var passEnc string
	_ = pool.QueryRow(ctx, `
		SELECT COALESCE(password_enc, '')
		FROM public.cto_database_credentials
		WHERE database_id = $1
	`, databaseID).Scan(&passEnc)
	password, _ := decrypt(passEnc)

	// Fall back to DATABASE_URL from environment when host is not set on the project
	if host == "" {
		envHost, envPort, envDB, envUser, envPass := parseDBURL(os.Getenv("DATABASE_URL"))
		if envHost == "" {
			return nil, fmt.Errorf("project has no host configured — edit the project and add the host")
		}
		host = envHost
		if port == 0 || port == 5432 {
			port = envPort
		}
		if dbName == "" {
			dbName = envDB
		}
		if username == "" {
			username = envUser
		}
		if password == "" {
			password = envPass
		}
	}

	// Try sslmode=require first (Supabase, RDS, Cloud SQL), then disable for local dev
	for _, sslmode := range []string{"require", "disable"} {
		dsn := fmt.Sprintf(
			"host=%s port=%d dbname=%s user=%s password=%s sslmode=%s",
			host, port, dbName, username, password, sslmode,
		)
		conn, connErr := pgx.Connect(ctx, dsn)
		if connErr == nil {
			return conn, nil
		}
	}

	return nil, fmt.Errorf("could not connect to %s:%d/%s — check credentials in the project settings", host, port, dbName)
}

// projectQuerier extracts database_id from the request, opens a connection to the
// external project DB, and returns it. cleanup() must be deferred by the caller.
func projectQuerier(w http.ResponseWriter, r *http.Request, dbID string) (dbQuerier, func(), bool) {
	if dbID == "" {
		utils_writeError(w, http.StatusBadRequest, "database_id is required")
		return nil, nil, false
	}
	conn, err := openProjectConn(r.Context(), dbID)
	if err != nil {
		utils_writeError(w, http.StatusBadGateway, err.Error())
		return nil, nil, false
	}
	return conn, func() { conn.Close(context.Background()) }, true
}

// parseDBURL extracts host, port, dbname, user, password from a postgres URL.
// Handles both postgres:// and postgresql:// schemes, and key=value DSN strings.
func parseDBURL(raw string) (host string, port int, dbName, user, password string) {
	port = 5432
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return
	}
	// Try URL format first
	if strings.HasPrefix(raw, "postgres://") || strings.HasPrefix(raw, "postgresql://") {
		u, err := url.Parse(raw)
		if err != nil {
			return
		}
		host = u.Hostname()
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				port = n
			}
		}
		user = u.User.Username()
		password, _ = u.User.Password()
		dbName = strings.TrimPrefix(u.Path, "/")
		return
	}
	// key=value DSN format
	for _, part := range strings.Fields(raw) {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch kv[0] {
		case "host":
			host = kv[1]
		case "port":
			if n, err := strconv.Atoi(kv[1]); err == nil {
				port = n
			}
		case "dbname":
			dbName = kv[1]
		case "user":
			user = kv[1]
		case "password":
			password = kv[1]
		}
	}
	return
}

func utils_writeError(w http.ResponseWriter, status int, msg string) {
	// thin shim so project_conn.go doesn't need to import utils (avoids cycle risk)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
