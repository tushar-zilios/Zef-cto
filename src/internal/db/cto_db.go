package db

import (
	"context"
	"fmt"
	"sync"
	"time"

	"cto/src/internal/logger"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ctoPool *pgxpool.Pool
	ctoOnce sync.Once
)

func InitCTODB(ctx context.Context, dbURL string) (*pgxpool.Pool, error) {
	if dbURL == "" {
		logger.LogDB("DATABASE_URL not set; skipping DB initialization")
		return nil, nil
	}

	var err error
	ctoOnce.Do(func() {
		logger.LogDB("Initializing CTO database pool...")

		config, parseErr := pgxpool.ParseConfig(dbURL)
		if parseErr != nil {
			err = fmt.Errorf("failed to parse CTO database URL: %w", parseErr)
			return
		}
		config.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

		retryErr := retryWithExponentialBackoff(ctx, 5, 1*time.Second, 30*time.Second, func() error {
			var connErr error
			ctoPool, connErr = pgxpool.NewWithConfig(ctx, config)
			if connErr != nil {
				return fmt.Errorf("failed to connect to CTO database: %w", connErr)
			}
			if pingErr := ctoPool.Ping(ctx); pingErr != nil {
				ctoPool.Close()
				ctoPool = nil
				return fmt.Errorf("failed to ping CTO database: %w", pingErr)
			}
			return nil
		}, func(format string, args ...any) {
			logger.LogDB(format, args...)
		})

		if retryErr != nil {
			err = fmt.Errorf("CTO database initialization failed after retries: %w", retryErr)
			return
		}
		logger.LogDB("CTO DB connection pool initialized successfully.")

		migrations := []struct {
			name string
			sql  string
		}{
			{"cto_database_projects", `
				CREATE TABLE IF NOT EXISTS public.cto_database_projects (
					database_id       UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
					workspace_id      UUID,
					created_by        UUID,
					name              VARCHAR(255) NOT NULL,
					description       TEXT         DEFAULT '',
					db_type           VARCHAR(50)  NOT NULL DEFAULT 'postgresql',
					host              VARCHAR(255) DEFAULT '',
					port              INTEGER      DEFAULT 5432,
					db_name           VARCHAR(255) DEFAULT '',
					username          VARCHAR(255) DEFAULT '',
					color             VARCHAR(20)  DEFAULT '#3ecf8e',
					is_active         BOOLEAN      NOT NULL DEFAULT true,
					last_connected_at TIMESTAMPTZ,
					created_at        TIMESTAMPTZ  DEFAULT NOW(),
					updated_at        TIMESTAMPTZ  DEFAULT NOW()
				);
			`},
			{"cto_database_projects_backfill", `
				ALTER TABLE public.cto_database_projects
					ADD COLUMN IF NOT EXISTS workspace_id      UUID,
					ADD COLUMN IF NOT EXISTS created_by        UUID,
					ADD COLUMN IF NOT EXISTS is_active         BOOLEAN     NOT NULL DEFAULT true,
					ADD COLUMN IF NOT EXISTS last_connected_at TIMESTAMPTZ;
				CREATE INDEX IF NOT EXISTS idx_cto_db_projects_workspace
					ON public.cto_database_projects(workspace_id);
			`},
			{"cto_database_credentials", `
				CREATE TABLE IF NOT EXISTS public.cto_database_credentials (
					credential_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
					database_id   UUID NOT NULL UNIQUE
					              REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					password_enc  TEXT DEFAULT '',
					ssl_cert      TEXT DEFAULT '',
					ssl_key       TEXT DEFAULT '',
					ssl_ca        TEXT DEFAULT '',
					updated_at    TIMESTAMPTZ DEFAULT NOW()
				);
			`},
			{"cto_connection_health", `
				CREATE TABLE IF NOT EXISTS public.cto_connection_health (
					health_id     UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
					database_id   UUID        NOT NULL
					              REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					checked_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
					ok            BOOLEAN     NOT NULL,
					latency_ms    INTEGER,
					error_message TEXT        DEFAULT ''
				);
				CREATE INDEX IF NOT EXISTS idx_cto_conn_health_db_checked
					ON public.cto_connection_health(database_id, checked_at DESC);
			`},
			{"cto_sql_history", `
				CREATE TABLE IF NOT EXISTS public.cto_sql_history (
					history_id    UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
					database_id   UUID        NOT NULL
					              REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					user_id       UUID,
					sql_text      TEXT        NOT NULL,
					execution_ms  INTEGER,
					row_count     INTEGER,
					had_error     BOOLEAN     NOT NULL DEFAULT false,
					error_message TEXT        DEFAULT '',
					executed_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_cto_sql_history_db_user
					ON public.cto_sql_history(database_id, user_id, executed_at DESC);
			`},
			{"cto_saved_queries", `
				CREATE TABLE IF NOT EXISTS public.cto_saved_queries (
					query_id     UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
					database_id  UUID         NOT NULL
					             REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					created_by   UUID,
					name         VARCHAR(255) NOT NULL,
					description  TEXT         DEFAULT '',
					sql_text     TEXT         NOT NULL,
					is_shared    BOOLEAN      NOT NULL DEFAULT false,
					created_at   TIMESTAMPTZ  DEFAULT NOW(),
					updated_at   TIMESTAMPTZ  DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_cto_saved_queries_db
					ON public.cto_saved_queries(database_id);
			`},
			{"cto_schema_snapshots", `
				CREATE TABLE IF NOT EXISTS public.cto_schema_snapshots (
					snapshot_id UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
					database_id UUID         NOT NULL
					            REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					captured_by UUID,
					label       VARCHAR(255) DEFAULT '',
					schema_json JSONB        NOT NULL DEFAULT '{}',
					captured_at TIMESTAMPTZ  NOT NULL DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_cto_schema_snapshots_db
					ON public.cto_schema_snapshots(database_id, captured_at DESC);
			`},
			{"cto_ideate_messages", `
				CREATE TABLE IF NOT EXISTS public.cto_ideate_messages (
					message_id UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
					user_id    UUID        NOT NULL,
					sender     VARCHAR(20) NOT NULL CHECK (sender IN ('user', 'assistant')),
					text       TEXT        NOT NULL,
					model      VARCHAR(100) DEFAULT '',
					created_at TIMESTAMPTZ  DEFAULT NOW()
				);
				CREATE INDEX IF NOT EXISTS idx_cto_ideate_messages_user_id
					ON public.cto_ideate_messages(user_id, created_at);
			`},
			{"workspace_to_database", `
				CREATE TABLE IF NOT EXISTS public.workspace_to_database (
					mapping_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
					workspace_id UUID        NOT NULL,
					database_id  UUID        NOT NULL
					             REFERENCES public.cto_database_projects(database_id) ON DELETE CASCADE,
					granted_by   UUID,
					created_at   TIMESTAMPTZ DEFAULT NOW(),
					UNIQUE (workspace_id, database_id)
				);
				CREATE INDEX IF NOT EXISTS idx_workspace_to_database_workspace
					ON public.workspace_to_database(workspace_id);
				CREATE INDEX IF NOT EXISTS idx_workspace_to_database_database
					ON public.workspace_to_database(database_id);
			`},
		}

		for _, m := range migrations {
			if _, mErr := ctoPool.Exec(ctx, m.sql); mErr != nil {
				logger.LogDB("Warning: %s migration: %v", m.name, mErr)
			}
		}
	})

	return ctoPool, err
}

func GetCTOPoolOrNil() *pgxpool.Pool {
	return ctoPool
}

func CTOPoolReady() bool {
	return ctoPool != nil
}

func CloseCTODB() {
	if ctoPool != nil {
		logger.LogDB("Closing CTO database connection pool.")
		ctoPool.Close()
	}
}
