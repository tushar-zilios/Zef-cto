package routes

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	dbproject "cto/src/internal/db/project"
	"cto/src/internal/logger"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5"
)

func handlerLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasSuffix(r.URL.Path, "/health") || strings.HasSuffix(r.URL.Path, "/health/") {
			next.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		logger.LogHandler("%s %s %s from %s - %d %dB in %s",
			r.Method, r.URL.Path, r.Proto, r.RemoteAddr,
			ww.Status(), ww.BytesWritten(), time.Since(start))
	})
}

func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			utils.WriteError(w, http.StatusUnauthorized, "Authorization header required")
			return
		}
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			utils.WriteError(w, http.StatusUnauthorized, "Authorization header must be in format Bearer <token>")
			return
		}
		claims, err := utils.VerifyToken(parts[1])
		if err != nil {
			utils.WriteError(w, http.StatusUnauthorized, "Invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "email", claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// ProjectMiddleware requires a valid project to be selected for the request.
// Reads X-Project-ID header, falls back to ?project_id= query param.
// Validates the project exists and stores it in context under "project_id".
func ProjectMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		projectID := r.Header.Get("X-Project-ID")
		if projectID == "" {
			projectID = r.URL.Query().Get("project_id")
		}
		if projectID == "" {
			utils.WriteError(w, http.StatusBadRequest, "X-Project-ID header (or project_id query param) is required")
			return
		}
		p, err := dbproject.GetProject(r.Context(), projectID)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				utils.WriteError(w, http.StatusNotFound, "Project not found")
				return
			}
			utils.WriteError(w, http.StatusInternalServerError, "Failed to resolve project: "+err.Error())
			return
		}
		ctx := context.WithValue(r.Context(), "project_id", p.ProjectID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func conditionalLogger(next http.Handler) http.Handler {
	loggerMW := middleware.Logger(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || strings.HasSuffix(r.URL.Path, "/health") || strings.HasSuffix(r.URL.Path, "/health/") {
			next.ServeHTTP(w, r)
			return
		}
		loggerMW.ServeHTTP(w, r)
	})
}

