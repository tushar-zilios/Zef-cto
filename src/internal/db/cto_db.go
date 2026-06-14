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

		_, execErrOrgs := ctoPool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS public.organizations (
				organization_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
				organization_name TEXT        NOT NULL,
				slug              TEXT        UNIQUE,
				logo_url          TEXT,
				created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
				updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
		`)
		if execErrOrgs != nil {
			logger.LogDB("Warning: failed to create organizations table: %v", execErrOrgs)
		}

		_, execErrProjects := ctoPool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS public.projects (
				project_id      UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
				name            TEXT,
				organization_id UUID        REFERENCES public.organizations(organization_id) ON DELETE SET NULL,
				created_by      UUID,
				created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
		`)
		if execErrProjects != nil {
			logger.LogDB("Warning: failed to create projects table: %v", execErrProjects)
		}

		_, execErrVault := ctoPool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS public.vault (
				credential_id   UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
				key             TEXT        NOT NULL,
				value           TEXT        NOT NULL,
				encryption_key  TEXT        NOT NULL,
				created_by      UUID,
				organization_id UUID        REFERENCES public.organizations(organization_id) ON DELETE SET NULL,
				project_id      UUID        REFERENCES public.projects(project_id) ON DELETE SET NULL,
				created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
		`)
		if execErrVault != nil {
			logger.LogDB("Warning: failed to create vault table: %v", execErrVault)
		}

		_, execErrGithub := ctoPool.Exec(ctx, `
			CREATE TABLE IF NOT EXISTS public.github_connections (
				user_id      UUID        PRIMARY KEY,
				github_login TEXT        NOT NULL,
				access_token TEXT        NOT NULL,
				connected_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
			);
		`)
		if execErrGithub != nil {
			logger.LogDB("Warning: failed to create github_connections table: %v", execErrGithub)
		}

		// migrate existing tables
		migrations := []string{
			`ALTER TABLE public.vault ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES public.projects(project_id) ON DELETE SET NULL`,
			`ALTER TABLE public.audit_logs ADD COLUMN IF NOT EXISTS project_id UUID REFERENCES public.projects(project_id) ON DELETE SET NULL`,
		}
		for _, m := range migrations {
			if _, err := ctoPool.Exec(ctx, m); err != nil {
				logger.LogDB("Warning: migration failed (%s): %v", m, err)
			}
		}

		_, seedErr := ctoPool.Exec(ctx, `
			INSERT INTO public.resources (resource_id, name, parent_id) VALUES
				('00000005-0000-0000-0000-000000000001', 'vault',             NULL),
				('00000005-0000-0000-0000-000000000003', 'audit_logs',        NULL),
				('00000005-0000-0000-0000-000000000002', 'vault.credentials', '00000005-0000-0000-0000-000000000001')
			ON CONFLICT (resource_id) DO NOTHING;
		`)
		if seedErr != nil {
			logger.LogDB("Warning: failed to seed CTO resources: %v", seedErr)
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
