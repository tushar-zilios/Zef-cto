package routes

import (
	"net/http"

	ctoHandlers "cto/src/internal/handlers/cto"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func NewRouter() http.Handler {
	r := chi.NewRouter()

	r.Use(utils.CORSMiddleware)
	r.Use(conditionalLogger)
	r.Use(handlerLogger)
	r.Use(middleware.Recoverer)

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"healthy"}`))
	})

	r.Route("/cto", func(sub chi.Router) {
		sub.Use(JWTMiddleware)

		sub.Post("/ideate/chat", ctoHandlers.IdeateHandler)
		sub.Get("/ideate/history", ctoHandlers.IdeateHistoryHandler)
		sub.Delete("/ideate/history", ctoHandlers.IdeateClearHistoryHandler)

		sub.Get("/projects", ctoHandlers.ListProjectsHandler)
		sub.Post("/projects", ctoHandlers.CreateProjectHandler)
		sub.Get("/projects/{id}", ctoHandlers.GetProjectHandler)
		sub.Put("/projects/{id}", ctoHandlers.UpdateProjectHandler)
		sub.Delete("/projects/{id}", ctoHandlers.DeleteProjectHandler)

		sub.Get("/projects/{id}/credentials", ctoHandlers.GetCredentialsMaskHandler)
		sub.Put("/projects/{id}/credentials", ctoHandlers.UpsertCredentialsHandler)
		sub.Delete("/projects/{id}/credentials", ctoHandlers.DeleteCredentialsHandler)

		sub.Post("/projects/{id}/test-connection", ctoHandlers.TestConnectionHandler)
		sub.Get("/projects/{id}/health", ctoHandlers.GetHealthHistoryHandler)

		sub.Get("/projects/{id}/sql-history", ctoHandlers.ListSQLHistoryHandler)
		sub.Delete("/projects/{id}/sql-history", ctoHandlers.ClearSQLHistoryHandler)

		sub.Get("/projects/{id}/saved-queries", ctoHandlers.ListSavedQueriesHandler)
		sub.Post("/projects/{id}/saved-queries", ctoHandlers.CreateSavedQueryHandler)
		sub.Put("/projects/{id}/saved-queries/{queryId}", ctoHandlers.UpdateSavedQueryHandler)
		sub.Delete("/projects/{id}/saved-queries/{queryId}", ctoHandlers.DeleteSavedQueryHandler)

		sub.Get("/projects/{id}/snapshots", ctoHandlers.ListSnapshotsHandler)
		sub.Post("/projects/{id}/snapshots", ctoHandlers.CaptureSnapshotHandler)
		sub.Get("/projects/{id}/snapshots/{snapshotId}", ctoHandlers.GetSnapshotHandler)
		sub.Delete("/projects/{id}/snapshots/{snapshotId}", ctoHandlers.DeleteSnapshotHandler)

		sub.Get("/db/schemas", ctoHandlers.ListSchemasHandler)
		sub.Get("/db/tables", ctoHandlers.ListTablesHandler)
		sub.Get("/db/tables/{schema}/{table}/columns", ctoHandlers.ListColumnsHandler)
		sub.Get("/db/tables/{schema}/{table}/rows", ctoHandlers.GetTableRowsHandler)
		sub.Post("/db/sql", ctoHandlers.ExecuteSQLHandler)

		sub.Get("/db/extensions", ctoHandlers.ListExtensionsHandler)
		sub.Get("/db/functions", ctoHandlers.ListFunctionsHandler)
		sub.Get("/db/triggers", ctoHandlers.ListTriggersHandler)
		sub.Get("/db/roles", ctoHandlers.ListRolesHandler)
		sub.Get("/db/views", ctoHandlers.ListViewsHandler)

		sub.Get("/projects/{id}/organizations", ctoHandlers.ListOrganizationsForDatabaseHandler)
		sub.Post("/projects/{id}/organizations", ctoHandlers.GrantOrganizationAccessHandler)
		sub.Delete("/projects/{id}/organizations/{organizationId}", ctoHandlers.RevokeOrganizationAccessHandler)

		sub.Get("/organization-databases", ctoHandlers.ListDatabasesForOrganizationHandler)

		sub.Get("/dbaas", ctoHandlers.ListDBaaSInstancesHandler)
		sub.Post("/dbaas", ctoHandlers.CreateDBaaSInstanceHandler)
		sub.Get("/dbaas/{instanceId}", ctoHandlers.GetDBaaSInstanceHandler)
		sub.Delete("/dbaas/{instanceId}", ctoHandlers.DeleteDBaaSInstanceHandler)

		sub.Get("/gcp/resources", ctoHandlers.GetGCPCloudMapHandler)
		sub.Get("/gcp/projects", ctoHandlers.ListGCPProjectsHandler)
		sub.Get("/gcp/sql/tiers", ctoHandlers.GetSQLTiersHandler)
		sub.Get("/gcp/regions", ctoHandlers.GetComputeRegionsHandler)

		// Deployment as a Service
		sub.Get("/deploy/apps", ctoHandlers.ListDeployAppsHandler)
		sub.Post("/deploy/apps", ctoHandlers.CreateDeployAppHandler)
		sub.Get("/deploy/apps/{appId}", ctoHandlers.GetDeployAppHandler)
		sub.Delete("/deploy/apps/{appId}", ctoHandlers.DeleteDeployAppHandler)
		sub.Post("/deploy/apps/{appId}/deploy", ctoHandlers.TriggerDeployHandler)
		sub.Get("/deploy/apps/{appId}/builds", ctoHandlers.ListBuildsHandler)
		sub.Get("/deploy/apps/{appId}/env", ctoHandlers.GetEnvVarsHandler)
		sub.Put("/deploy/apps/{appId}/env", ctoHandlers.SetEnvVarsHandler)
		sub.Get("/deploy/builds/{buildId}/logs", ctoHandlers.GetBuildLogsSSEHandler)
	})

	// GitHub webhook — no JWT, uses HMAC signature
	r.Post("/webhooks/deploy/{appId}", ctoHandlers.WebhookGitHubHandler)

	return r
}
