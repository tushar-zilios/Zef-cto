package routes

import (
	"net/http"

	auditHandlers "cto/src/internal/handlers/audit"
	githubHandlers "cto/src/internal/handlers/github"
	orgHandlers "cto/src/internal/handlers/organization"
	projectHandlers "cto/src/internal/handlers/project"
	vaultHandlers "cto/src/internal/handlers/vault"
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

	r.Route("/organizations", func(r chi.Router) {
		r.Use(JWTMiddleware)
		r.Post("/provision", orgHandlers.ProvisionOrganizationHandler)
		r.Get("/{id}", orgHandlers.GetOrganizationHandler)
	})

	r.Route("/vault", func(r chi.Router) {
		r.Use(JWTMiddleware)
		r.Use(ProjectMiddleware)

		r.Post("/", vaultHandlers.CreateVaultSecretHandler)
		r.Get("/", vaultHandlers.ListVaultSecretsHandler)
		r.Get("/{id}", vaultHandlers.GetVaultSecretHandler)
		r.Put("/{id}", vaultHandlers.UpdateVaultSecretHandler)
		r.Delete("/{id}", vaultHandlers.DeleteVaultSecretHandler)

		// /vault/secrets/** — alias used by the frontend
		r.Route("/secrets", func(r chi.Router) {
			r.Post("/", vaultHandlers.CreateVaultSecretHandler)
			r.Get("/", vaultHandlers.ListVaultSecretsHandler)
			r.Get("/{id}", vaultHandlers.GetVaultSecretHandler)
			r.Put("/{id}", vaultHandlers.UpdateVaultSecretHandler)
			r.Delete("/{id}", vaultHandlers.DeleteVaultSecretHandler)
		})
	})

	// /audit-logs/** — alias used by the frontend
	r.Route("/audit-logs", func(r chi.Router) {
		r.Use(JWTMiddleware)
		r.Use(ProjectMiddleware)
		r.Get("/", auditHandlers.ListAuditLogsHandler)
		r.Get("/count", auditHandlers.CountAuditLogsHandler)
		r.Get("/{id}", auditHandlers.GetAuditLogHandler)
		r.Delete("/{id}", auditHandlers.DeleteAuditLogHandler)
	})

	r.Route("/projects", func(r chi.Router) {
		r.Use(JWTMiddleware)
		// project CRUD does not require a project to already be selected
		r.Post("/", projectHandlers.CreateProjectHandler)
		r.Get("/", projectHandlers.ListProjectsHandler)
		r.Get("/{id}", projectHandlers.GetProjectHandler)
		r.Delete("/{id}", projectHandlers.DeleteProjectHandler)

		// project-scoped resource views
		r.Get("/{id}/vault", projectHandlers.ListProjectVaultHandler)
		r.Get("/{id}/audit-logs", projectHandlers.ListProjectAuditLogsHandler)
	})

	// GitHub OAuth — /github/connect is a browser redirect so it takes JWT via ?token=
	r.Get("/github/connect", githubHandlers.ConnectHandler)
	r.Get("/github/callback", githubHandlers.CallbackHandler)
	r.Route("/github", func(r chi.Router) {
		r.Use(JWTMiddleware)
		r.Get("/status", githubHandlers.StatusHandler)
		r.Get("/repos", githubHandlers.ReposHandler)
		r.Delete("/disconnect", githubHandlers.DisconnectHandler)
	})

	r.Route("/audit", func(r chi.Router) {
		r.Use(JWTMiddleware)
		r.Use(ProjectMiddleware)

		r.Route("/resources", func(r chi.Router) {
			r.Post("/", auditHandlers.CreateResourceHandler)
			r.Get("/", auditHandlers.ListResourcesHandler)
			r.Get("/{id}", auditHandlers.GetResourceHandler)
			r.Put("/{id}", auditHandlers.UpdateResourceHandler)
			r.Delete("/{id}", auditHandlers.DeleteResourceHandler)
		})

		r.Route("/logs", func(r chi.Router) {
			r.Post("/", auditHandlers.CreateAuditLogHandler)
			r.Get("/", auditHandlers.ListAuditLogsHandler)
			r.Get("/count", auditHandlers.CountAuditLogsHandler)
			r.Get("/{id}", auditHandlers.GetAuditLogHandler)
			r.Delete("/{id}", auditHandlers.DeleteAuditLogHandler)
		})
	})

	return r
}
