package routes

import (
	"context"
	"net/http"
	"strings"

	"cto/src/internal/utils"
)

func JWTMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		var tokenStr string
		if authHeader == "" {
			tokenStr = r.URL.Query().Get("token")
			if tokenStr == "" {
				utils.WriteError(w, http.StatusUnauthorized, "Authorization header or token query parameter is required")
				return
			}
		} else {
			parts := strings.Split(authHeader, " ")
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				utils.WriteError(w, http.StatusUnauthorized, "Authorization header must be in format Bearer <token>")
				return
			}
			tokenStr = parts[1]
		}

		claims, err := utils.VerifyToken(tokenStr)
		if err != nil {
			utils.WriteError(w, http.StatusUnauthorized, "Invalid token: "+err.Error())
			return
		}

		ctx := context.WithValue(r.Context(), "user_id", claims.UserID)
		ctx = context.WithValue(ctx, "email", claims.Email)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
