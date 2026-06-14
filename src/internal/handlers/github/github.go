package github

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	db "cto/src/internal/db"
	"cto/src/internal/logger"
	"cto/src/internal/utils"
)

// in-memory state store: state token → userID, expires after 10 minutes
var (
	stateMu    sync.Mutex
	stateStore = map[string]stateEntry{}
)

type stateEntry struct {
	userID    string
	expiresAt time.Time
}

func saveState(state, userID string) {
	stateMu.Lock()
	defer stateMu.Unlock()
	// prune expired entries
	now := time.Now()
	for k, v := range stateStore {
		if now.After(v.expiresAt) {
			delete(stateStore, k)
		}
	}
	stateStore[state] = stateEntry{userID: userID, expiresAt: now.Add(10 * time.Minute)}
}

func popState(state string) (string, bool) {
	stateMu.Lock()
	defer stateMu.Unlock()
	entry, ok := stateStore[state]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(stateStore, state)
		return "", false
	}
	delete(stateStore, state)
	return entry.userID, true
}

// ConnectHandler initiates the GitHub OAuth flow.
// Accepts JWT via ?token= query param (so it works as a popup redirect target).
// GET /github/connect?token=<jwt>
func ConnectHandler(w http.ResponseWriter, r *http.Request) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	if clientID == "" {
		utils.WriteError(w, http.StatusServiceUnavailable, "GitHub OAuth not configured")
		return
	}

	// validate JWT from query param (used because this is a browser redirect, not XHR)
	tokenStr := r.URL.Query().Get("token")
	if tokenStr == "" {
		utils.WriteError(w, http.StatusUnauthorized, "token query param required")
		return
	}
	claims, err := utils.VerifyToken(tokenStr)
	if err != nil {
		utils.WriteError(w, http.StatusUnauthorized, "Invalid token")
		return
	}

	state := fmt.Sprintf("%s-%d", claims.UserID, time.Now().UnixNano())
	saveState(state, claims.UserID)

	redirectURI := os.Getenv("GITHUB_REDIRECT_URI")
	if redirectURI == "" {
		port := os.Getenv("PORT")
		if port == "" {
			port = "8081"
		}
		redirectURI = "http://localhost:" + port + "/github/callback"
	}

	params := url.Values{}
	params.Set("client_id", clientID)
	params.Set("redirect_uri", redirectURI)
	params.Set("scope", "repo read:user read:org")
	params.Set("state", state)

	http.Redirect(w, r, "https://github.com/login/oauth/authorize?"+params.Encode(), http.StatusTemporaryRedirect)
}

// CallbackHandler handles the GitHub OAuth callback.
// GET /github/callback?code=<code>&state=<state>
func CallbackHandler(w http.ResponseWriter, r *http.Request) {
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")

	frontendURL := os.Getenv("FRONTEND_URL")
	if frontendURL == "" {
		frontendURL = "http://localhost:5173"
	}

	closePage := func(success bool, errMsg string) {
		msg := "false"
		if success {
			msg = "true"
		}
		errJS := "null"
		if errMsg != "" {
			errJS = fmt.Sprintf(`"%s"`, strings.ReplaceAll(errMsg, `"`, `\"`))
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><body><script>
if (window.opener) {
  window.opener.postMessage({ type: 'github_oauth', success: %s, error: %s }, '%s');
}
window.close();
</script></body></html>`, msg, errJS, frontendURL)
	}

	if errParam := r.URL.Query().Get("error"); errParam != "" {
		closePage(false, r.URL.Query().Get("error_description"))
		return
	}

	userID, ok := popState(state)
	if !ok {
		closePage(false, "Invalid or expired OAuth state")
		return
	}

	// exchange code for access token
	accessToken, login, err := exchangeCode(code)
	if err != nil {
		logger.LogHandler("GitHub OAuth exchange failed: %v", err)
		closePage(false, "Failed to exchange code: "+err.Error())
		return
	}

	if err := upsertConnection(r.Context(), userID, login, accessToken); err != nil {
		logger.LogHandler("GitHub upsert failed: %v", err)
		closePage(false, "Failed to save GitHub connection")
		return
	}

	closePage(true, "")
}

// StatusHandler returns the current GitHub connection for the authenticated user.
// GET /github/status  (JWT required)
func StatusHandler(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value("user_id").(string)
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		utils.WriteJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	var login string
	var connectedAt time.Time
	err := pool.QueryRow(r.Context(),
		`SELECT github_login, connected_at FROM public.github_connections WHERE user_id = $1`, userID,
	).Scan(&login, &connectedAt)
	if err != nil {
		utils.WriteJSON(w, http.StatusOK, map[string]any{"connected": false})
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]any{
		"connected":    true,
		"github_login": login,
		"connected_at": connectedAt,
	})
}

// DisconnectHandler removes the GitHub connection for the authenticated user.
// DELETE /github/disconnect  (JWT required)
func DisconnectHandler(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value("user_id").(string)
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		utils.WriteError(w, http.StatusServiceUnavailable, "Database unavailable")
		return
	}
	_, err := pool.Exec(r.Context(),
		`DELETE FROM public.github_connections WHERE user_id = $1`, userID,
	)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "Failed to disconnect GitHub")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

// ReposHandler returns all GitHub repositories for the authenticated user's connected account.
// GET /github/repos  (JWT required)
func ReposHandler(w http.ResponseWriter, r *http.Request) {
	userID, _ := r.Context().Value("user_id").(string)
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		utils.WriteError(w, http.StatusServiceUnavailable, "Database unavailable")
		return
	}
	var accessToken string
	err := pool.QueryRow(r.Context(),
		`SELECT access_token FROM public.github_connections WHERE user_id = $1`, userID,
	).Scan(&accessToken)
	if err != nil {
		utils.WriteError(w, http.StatusUnauthorized, "GitHub account not connected")
		return
	}

	repos, err := fetchAllRepos(accessToken)
	if err != nil {
		logger.LogHandler("GitHub repos fetch failed: %v", err)
		utils.WriteError(w, http.StatusBadGateway, "Failed to fetch repositories from GitHub")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]any{"repos": repos})
}

// --- helpers ---

func exchangeCode(code string) (accessToken, login string, err error) {
	clientID := os.Getenv("GITHUB_CLIENT_ID")
	clientSecret := os.Getenv("GITHUB_CLIENT_SECRET")

	body := url.Values{}
	body.Set("client_id", clientID)
	body.Set("client_secret", clientSecret)
	body.Set("code", code)

	req, _ := http.NewRequest("POST", "https://github.com/login/oauth/access_token", strings.NewReader(body.Encode()))
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
		ErrorDesc   string `json:"error_description"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", "", err
	}
	if tokenResp.Error != "" {
		return "", "", fmt.Errorf("%s: %s", tokenResp.Error, tokenResp.ErrorDesc)
	}

	// fetch GitHub username
	userReq, _ := http.NewRequest("GET", "https://api.github.com/user", nil)
	userReq.Header.Set("Authorization", "Bearer "+tokenResp.AccessToken)
	userReq.Header.Set("Accept", "application/vnd.github+json")

	userResp, err := http.DefaultClient.Do(userReq)
	if err != nil {
		return "", "", err
	}
	defer userResp.Body.Close()

	var ghUser struct {
		Login string `json:"login"`
	}
	if err := json.NewDecoder(userResp.Body).Decode(&ghUser); err != nil {
		return "", "", err
	}

	return tokenResp.AccessToken, ghUser.Login, nil
}

type GitHubRepo struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Private     bool   `json:"private"`
	HTMLURL     string `json:"html_url"`
	Description string `json:"description"`
	UpdatedAt   string `json:"updated_at"`
	Language    string `json:"language"`
}

func fetchAllRepos(accessToken string) ([]GitHubRepo, error) {
	var all []GitHubRepo
	pageNum := 1
	for {
		apiURL := fmt.Sprintf("https://api.github.com/user/repos?per_page=100&page=%d&sort=updated", pageNum)
		req, _ := http.NewRequest("GET", apiURL, nil)
		req.Header.Set("Authorization", "Bearer "+accessToken)
		req.Header.Set("Accept", "application/vnd.github+json")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		var batch []GitHubRepo
		if err := json.NewDecoder(resp.Body).Decode(&batch); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()
		if len(batch) == 0 {
			break
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
		pageNum++
	}
	return all, nil
}

func upsertConnection(ctx context.Context, userID, login, accessToken string) error {
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return fmt.Errorf("database unavailable")
	}
	_, err := pool.Exec(ctx, `
		INSERT INTO public.github_connections (user_id, github_login, access_token)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE
		SET github_login = EXCLUDED.github_login,
		    access_token = EXCLUDED.access_token,
		    connected_at = NOW()
	`, userID, login, accessToken)
	return err
}
