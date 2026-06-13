package cto

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/logger"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

// ─── Models ──────────────────────────────────────────────────────────────────

type DeployApp struct {
	AppID            string     `json:"app_id"`
	WorkspaceID      string     `json:"workspace_id"`
	CreatedBy        *string    `json:"created_by,omitempty"`
	Name             string     `json:"name"`
	RepoURL          string     `json:"repo_url"`
	Branch           string     `json:"branch"`
	Status           string     `json:"status"`
	ServiceURL       string     `json:"service_url"`
	CloudRunService  string     `json:"cloud_run_service"`
	GCPProject       string     `json:"gcp_project"`
	Region           string     `json:"region"`
	WebhookSecret    string     `json:"webhook_secret,omitempty"`
	LastDeployedAt   *time.Time `json:"last_deployed_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type DeployBuild struct {
	BuildID     string     `json:"build_id"`
	AppID       string     `json:"app_id"`
	TriggeredBy string     `json:"triggered_by"`
	CommitSHA   string     `json:"commit_sha"`
	CommitMsg   string     `json:"commit_msg"`
	Status      string     `json:"status"`
	ImageURL    string     `json:"image_url"`
	ErrorMsg    string     `json:"error_msg"`
	StartedAt   time.Time  `json:"started_at"`
	FinishedAt  *time.Time `json:"finished_at,omitempty"`
}

type EnvVar struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

// ─── SSE log broker ──────────────────────────────────────────────────────────

type logBroker struct {
	mu   sync.RWMutex
	subs map[string][]chan string // buildID → listeners
}

var globalLogBroker = &logBroker{subs: make(map[string][]chan string)}

func (b *logBroker) subscribe(buildID string) chan string {
	ch := make(chan string, 256)
	b.mu.Lock()
	b.subs[buildID] = append(b.subs[buildID], ch)
	b.mu.Unlock()
	return ch
}

func (b *logBroker) unsubscribe(buildID string, ch chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	subs := b.subs[buildID]
	for i, c := range subs {
		if c == ch {
			b.subs[buildID] = append(subs[:i], subs[i+1:]...)
			break
		}
	}
	if len(b.subs[buildID]) == 0 {
		delete(b.subs, buildID)
	}
}

func (b *logBroker) publish(buildID, stream, msg string) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	event := fmt.Sprintf(`{"stream":%q,"msg":%q,"ts":%q}`, stream, msg, time.Now().UTC().Format(time.RFC3339))
	for _, ch := range b.subs[buildID] {
		select {
		case ch <- event:
		default:
		}
	}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func deployGCPProject() string {
	if p := os.Getenv("GCP_PROJECT_ID"); p != "" {
		return p
	}
	return "bhanshu"
}

func gcrRegistry() string {
	if r := os.Getenv("GCR_REGISTRY"); r != "" {
		return r
	}
	return "gcr.io/" + deployGCPProject()
}

func deployRegion() string {
	if r := os.Getenv("DEPLOY_REGION"); r != "" {
		return r
	}
	return "us-central1"
}

func randomSecret() string {
	b := make([]byte, 20)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func sanitizeServiceName(name string) string {
	s := strings.ToLower(name)
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "_", "-")
	// keep only alphanumeric and hyphens
	var out []byte
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			out = append(out, c)
		}
	}
	return string(out)
}

// persistLog writes a log line to DB and broadcasts via SSE broker.
func persistLog(buildID, stream, msg string) {
	globalLogBroker.publish(buildID, stream, msg)
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return
	}
	pool.Exec(context.Background(), `
		INSERT INTO public.deployment_build_logs (build_id, stream, message)
		VALUES ($1, $2, $3)
	`, buildID, stream, msg)
}

func updateBuildStatus(buildID, status, errMsg, imageURL string, done bool) {
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return
	}
	if done {
		pool.Exec(context.Background(), `
			UPDATE public.deployment_builds
			SET status=$1, error_msg=$2, image_url=$3, finished_at=NOW()
			WHERE build_id=$4
		`, status, errMsg, imageURL, buildID)
	} else {
		pool.Exec(context.Background(), `
			UPDATE public.deployment_builds SET status=$1 WHERE build_id=$2
		`, status, buildID)
	}
}

func updateAppStatus(appID, status, serviceURL string) {
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return
	}
	if serviceURL != "" {
		pool.Exec(context.Background(), `
			UPDATE public.deployment_apps
			SET status=$1, service_url=$2, last_deployed_at=NOW(), updated_at=NOW()
			WHERE app_id=$3
		`, status, serviceURL, appID)
	} else {
		pool.Exec(context.Background(), `
			UPDATE public.deployment_apps SET status=$1, updated_at=NOW() WHERE app_id=$2
		`, status, appID)
	}
}

// ─── Build worker ─────────────────────────────────────────────────────────────

func runBuild(buildID, appID, repoURL, branch, commitSHA, region, gcpProject, crService string, envVars []EnvVar) {
	log := func(stream, msg string) {
		logger.LogHandler("[deploy:%s] %s", buildID[:8], msg)
		persistLog(buildID, stream, msg)
	}

	log("system", fmt.Sprintf("Build started — repo: %s branch: %s", repoURL, branch))
	updateBuildStatus(buildID, "building", "", "", false)
	updateAppStatus(appID, "building", "")

	// 1. Clone repo
	workDir, err := os.MkdirTemp("", "zef-build-*")
	if err != nil {
		log("stderr", "Failed to create temp dir: "+err.Error())
		updateBuildStatus(buildID, "failed", err.Error(), "", true)
		updateAppStatus(appID, "failed", "")
		return
	}
	defer os.RemoveAll(workDir)

	log("system", "Cloning repository...")
	cloneArgs := []string{"clone", "--depth=1", "--branch", branch, repoURL, workDir}
	if out, err := runCmd(log, "git", cloneArgs...); err != nil {
		log("stderr", string(out))
		updateBuildStatus(buildID, "failed", "clone failed: "+err.Error(), "", true)
		updateAppStatus(appID, "failed", "")
		return
	}

	// Resolve commit SHA if not provided
	if commitSHA == "" {
		if out, err := exec.Command("git", "-C", workDir, "rev-parse", "--short", "HEAD").Output(); err == nil {
			commitSHA = strings.TrimSpace(string(out))
		}
	}
	db.GetCTOPoolOrNil().Exec(context.Background(), `UPDATE public.deployment_builds SET commit_sha=$1 WHERE build_id=$2`, commitSHA, buildID)

	// 2. Build image with nixpacks
	imageTag := fmt.Sprintf("%s/%s:%s", gcrRegistry(), sanitizeServiceName(crService), commitSHA)
	log("system", "Building image with Nixpacks → "+imageTag)
	updateBuildStatus(buildID, "pushing", "", "", false)

	nixpacksBin := "nixpacks"
	if p := os.Getenv("NIXPACKS_PATH"); p != "" {
		nixpacksBin = p
	}
	if out, err := runCmd(log, nixpacksBin, "build", workDir, "--name", imageTag); err != nil {
		log("stderr", string(out))
		// Fallback: try plain docker build if there's a Dockerfile
		dockerfilePath := workDir + "/Dockerfile"
		if _, ferr := os.Stat(dockerfilePath); ferr == nil {
			log("system", "Nixpacks failed, falling back to Dockerfile build...")
			if out2, err2 := runCmd(log, "docker", "build", "-t", imageTag, workDir); err2 != nil {
				log("stderr", string(out2))
				updateBuildStatus(buildID, "failed", "build failed: "+err2.Error(), "", true)
				updateAppStatus(appID, "failed", "")
				return
			}
		} else {
			updateBuildStatus(buildID, "failed", "build failed: "+err.Error(), "", true)
			updateAppStatus(appID, "failed", "")
			return
		}
	}

	// 3. Configure docker for GCR and push
	log("system", "Configuring Docker for GCR...")
	exec.Command("gcloud", "auth", "configure-docker", "--quiet").Run()

	log("system", "Pushing image to registry...")
	if out, err := runCmd(log, "docker", "push", imageTag); err != nil {
		log("stderr", string(out))
		updateBuildStatus(buildID, "failed", "push failed: "+err.Error(), "", true)
		updateAppStatus(appID, "failed", "")
		return
	}

	// 4. Deploy to Cloud Run
	log("system", fmt.Sprintf("Deploying to Cloud Run service '%s' in %s...", crService, region))
	updateBuildStatus(buildID, "deploying", "", imageTag, false)
	updateAppStatus(appID, "deploying", "")

	deployArgs := []string{
		"run", "deploy", crService,
		"--image=" + imageTag,
		"--platform=managed",
		"--region=" + region,
		"--project=" + gcpProject,
		"--allow-unauthenticated",
		"--quiet",
	}

	// Inject env vars
	if len(envVars) > 0 {
		pairs := make([]string, 0, len(envVars))
		for _, ev := range envVars {
			pairs = append(pairs, ev.Key+"="+ev.Value)
		}
		deployArgs = append(deployArgs, "--set-env-vars="+strings.Join(pairs, ","))
	}

	if out, err := runCmd(log, "gcloud", deployArgs...); err != nil {
		log("stderr", string(out))
		updateBuildStatus(buildID, "failed", "deploy failed: "+err.Error(), imageTag, true)
		updateAppStatus(appID, "failed", "")
		return
	}

	// 5. Get service URL
	serviceURL := ""
	descOut, descErr := exec.Command("gcloud", "run", "services", "describe", crService,
		"--region="+region, "--project="+gcpProject, "--format=json",
	).Output()
	if descErr == nil {
		var desc map[string]any
		if json.Unmarshal(descOut, &desc) == nil {
			if status, ok := desc["status"].(map[string]any); ok {
				serviceURL, _ = status["url"].(string)
			}
		}
	}

	log("system", fmt.Sprintf("Deployment successful! URL: %s", serviceURL))
	updateBuildStatus(buildID, "healthy", "", imageTag, true)
	updateAppStatus(appID, "healthy", serviceURL)
}

// runCmd executes a command and streams stdout/stderr to the log function.
func runCmd(log func(string, string), name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stdout.Read(buf)
			if n > 0 {
				log("stdout", strings.TrimRight(string(buf[:n]), "\n"))
			}
			if err != nil {
				break
			}
		}
	}()
	go func() {
		defer wg.Done()
		buf := make([]byte, 4096)
		for {
			n, err := stderr.Read(buf)
			if n > 0 {
				log("stderr", strings.TrimRight(string(buf[:n]), "\n"))
			}
			if err != nil {
				break
			}
		}
	}()
	wg.Wait()

	err := cmd.Wait()
	return nil, err
}

// ─── Handlers ────────────────────────────────────────────────────────────────

func ListDeployAppsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT app_id, workspace_id, created_by, name, repo_url, branch,
		       status, service_url, cloud_run_service, gcp_project, region,
		       webhook_secret, last_deployed_at, created_at, updated_at
		FROM public.deployment_apps
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var apps []DeployApp
	for rows.Next() {
		var a DeployApp
		if err := rows.Scan(
			&a.AppID, &a.WorkspaceID, &a.CreatedBy, &a.Name, &a.RepoURL, &a.Branch,
			&a.Status, &a.ServiceURL, &a.CloudRunService, &a.GCPProject, &a.Region,
			&a.WebhookSecret, &a.LastDeployedAt, &a.CreatedAt, &a.UpdatedAt,
		); err != nil {
			continue
		}
		apps = append(apps, a)
	}
	if apps == nil {
		apps = []DeployApp{}
	}
	utils.WriteJSON(w, http.StatusOK, apps)
}

func CreateDeployAppHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var input struct {
		Name    string `json:"name"`
		RepoURL string `json:"repo_url"`
		Branch  string `json:"branch"`
		Region  string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Name == "" || input.RepoURL == "" {
		utils.WriteError(w, http.StatusBadRequest, "name and repo_url are required")
		return
	}
	if input.Branch == "" {
		input.Branch = "main"
	}
	if input.Region == "" {
		input.Region = deployRegion()
	}

	userID, _ := r.Context().Value("user_id").(string)
	crService := "zef-" + sanitizeServiceName(input.Name)
	secret := randomSecret()
	project := deployGCPProject()

	var app DeployApp
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.deployment_apps
			(workspace_id, created_by, name, repo_url, branch, region, cloud_run_service, gcp_project, webhook_secret)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9)
		RETURNING app_id, workspace_id, created_by, name, repo_url, branch,
		          status, service_url, cloud_run_service, gcp_project, region,
		          webhook_secret, last_deployed_at, created_at, updated_at
	`, workspaceID, userID, input.Name, input.RepoURL, input.Branch, input.Region, crService, project, secret,
	).Scan(
		&app.AppID, &app.WorkspaceID, &app.CreatedBy, &app.Name, &app.RepoURL, &app.Branch,
		&app.Status, &app.ServiceURL, &app.CloudRunService, &app.GCPProject, &app.Region,
		&app.WebhookSecret, &app.LastDeployedAt, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, app)
}

func GetDeployAppHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")
	var app DeployApp
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		SELECT app_id, workspace_id, created_by, name, repo_url, branch,
		       status, service_url, cloud_run_service, gcp_project, region,
		       webhook_secret, last_deployed_at, created_at, updated_at
		FROM public.deployment_apps WHERE app_id = $1
	`, appID).Scan(
		&app.AppID, &app.WorkspaceID, &app.CreatedBy, &app.Name, &app.RepoURL, &app.Branch,
		&app.Status, &app.ServiceURL, &app.CloudRunService, &app.GCPProject, &app.Region,
		&app.WebhookSecret, &app.LastDeployedAt, &app.CreatedAt, &app.UpdatedAt,
	)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "app not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, app)
}

func DeleteDeployAppHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")
	_, err := db.GetCTOPoolOrNil().Exec(r.Context(), `DELETE FROM public.deployment_apps WHERE app_id = $1`, appID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "app deleted"})
}

func TriggerDeployHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")
	pool := db.GetCTOPoolOrNil()

	var app DeployApp
	err := pool.QueryRow(r.Context(), `
		SELECT app_id, workspace_id, name, repo_url, branch, status,
		       cloud_run_service, gcp_project, region
		FROM public.deployment_apps WHERE app_id = $1
	`, appID).Scan(
		&app.AppID, &app.WorkspaceID, &app.Name, &app.RepoURL, &app.Branch, &app.Status,
		&app.CloudRunService, &app.GCPProject, &app.Region,
	)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "app not found")
		return
	}

	// Fetch env vars
	envRows, _ := pool.Query(r.Context(), `SELECT key, value FROM public.deployment_env_vars WHERE app_id = $1`, appID)
	var envVars []EnvVar
	if envRows != nil {
		defer envRows.Close()
		for envRows.Next() {
			var ev EnvVar
			if envRows.Scan(&ev.Key, &ev.Value) == nil {
				envVars = append(envVars, ev)
			}
		}
	}

	// Create build record
	var buildID string
	err = pool.QueryRow(r.Context(), `
		INSERT INTO public.deployment_builds (app_id, triggered_by, status)
		VALUES ($1, 'manual', 'pending')
		RETURNING build_id
	`, appID).Scan(&buildID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	go runBuild(buildID, appID, app.RepoURL, app.Branch, "", app.Region, app.GCPProject, app.CloudRunService, envVars)

	utils.WriteJSON(w, http.StatusAccepted, map[string]string{"build_id": buildID, "status": "pending"})
}

func ListBuildsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT build_id, app_id, triggered_by, commit_sha, commit_msg, status,
		       image_url, error_msg, started_at, finished_at
		FROM public.deployment_builds
		WHERE app_id = $1
		ORDER BY started_at DESC
		LIMIT 50
	`, appID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var builds []DeployBuild
	for rows.Next() {
		var b DeployBuild
		if err := rows.Scan(
			&b.BuildID, &b.AppID, &b.TriggeredBy, &b.CommitSHA, &b.CommitMsg,
			&b.Status, &b.ImageURL, &b.ErrorMsg, &b.StartedAt, &b.FinishedAt,
		); err != nil {
			continue
		}
		builds = append(builds, b)
	}
	if builds == nil {
		builds = []DeployBuild{}
	}
	utils.WriteJSON(w, http.StatusOK, builds)
}

func GetBuildLogsSSEHandler(w http.ResponseWriter, r *http.Request) {
	buildID := chi.URLParam(r, "buildId")

	// Flush headers for SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	flusher, ok := w.(http.Flusher)
	if !ok {
		return
	}

	// Send historical logs first
	pool := db.GetCTOPoolOrNil()
	if pool != nil {
		rows, err := pool.Query(r.Context(), `
			SELECT stream, message, ts
			FROM public.deployment_build_logs
			WHERE build_id = $1
			ORDER BY ts ASC
		`, buildID)
		if err == nil {
			defer rows.Close()
			for rows.Next() {
				var stream, message, ts string
				if rows.Scan(&stream, &message, &ts) == nil {
					data := fmt.Sprintf(`{"stream":%q,"msg":%q,"ts":%q}`, stream, message, ts)
					fmt.Fprintf(w, "data: %s\n\n", data)
				}
			}
			flusher.Flush()
		}
	}

	// Check if build is already finished
	var buildStatus string
	if pool != nil {
		pool.QueryRow(r.Context(), `SELECT status FROM public.deployment_builds WHERE build_id = $1`, buildID).Scan(&buildStatus)
	}
	if buildStatus == "healthy" || buildStatus == "failed" {
		fmt.Fprintf(w, "data: {\"done\":true}\n\n")
		flusher.Flush()
		return
	}

	// Subscribe to live logs
	ch := globalLogBroker.subscribe(buildID)
	defer globalLogBroker.unsubscribe(buildID, ch)

	ctx := r.Context()
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		case <-ticker.C:
			// heartbeat
			fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()

			// check if build is done
			if pool != nil {
				var s string
				pool.QueryRow(context.Background(), `SELECT status FROM public.deployment_builds WHERE build_id = $1`, buildID).Scan(&s)
				if s == "healthy" || s == "failed" {
					fmt.Fprintf(w, "data: {\"done\":true}\n\n")
					flusher.Flush()
					return
				}
			}
		}
	}
}

func GetEnvVarsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT key, value FROM public.deployment_env_vars WHERE app_id = $1 ORDER BY key
	`, appID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var vars []EnvVar
	for rows.Next() {
		var ev EnvVar
		if rows.Scan(&ev.Key, &ev.Value) == nil {
			vars = append(vars, ev)
		}
	}
	if vars == nil {
		vars = []EnvVar{}
	}
	utils.WriteJSON(w, http.StatusOK, vars)
}

func SetEnvVarsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	appID := chi.URLParam(r, "appId")

	var vars []EnvVar
	if err := json.NewDecoder(r.Body).Decode(&vars); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid body")
		return
	}

	pool := db.GetCTOPoolOrNil()
	pool.Exec(r.Context(), `DELETE FROM public.deployment_env_vars WHERE app_id = $1`, appID)
	for _, ev := range vars {
		if ev.Key == "" {
			continue
		}
		pool.Exec(r.Context(), `
			INSERT INTO public.deployment_env_vars (app_id, key, value) VALUES ($1, $2, $3)
		`, appID, ev.Key, ev.Value)
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "env vars updated"})
}

func WebhookGitHubHandler(w http.ResponseWriter, r *http.Request) {
	appID := chi.URLParam(r, "appId")
	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		utils.WriteError(w, http.StatusServiceUnavailable, "database unavailable")
		return
	}

	var app DeployApp
	err := pool.QueryRow(r.Context(), `
		SELECT app_id, repo_url, branch, status, cloud_run_service, gcp_project, region, webhook_secret
		FROM public.deployment_apps WHERE app_id = $1
	`, appID).Scan(
		&app.AppID, &app.RepoURL, &app.Branch, &app.Status,
		&app.CloudRunService, &app.GCPProject, &app.Region, &app.WebhookSecret,
	)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "app not found")
		return
	}

	// Verify GitHub signature
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		utils.WriteError(w, http.StatusBadRequest, "cannot read body")
		return
	}

	sig := r.Header.Get("X-Hub-Signature-256")
	if app.WebhookSecret != "" && sig != "" {
		mac := hmac.New(sha256.New, []byte(app.WebhookSecret))
		mac.Write(body)
		expected := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(sig), []byte(expected)) {
			utils.WriteError(w, http.StatusUnauthorized, "invalid signature")
			return
		}
	}

	// Parse push event
	var payload struct {
		Ref        string `json:"ref"`
		HeadCommit struct {
			ID      string `json:"id"`
			Message string `json:"message"`
		} `json:"head_commit"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	// Only trigger on the configured branch
	expectedRef := "refs/heads/" + app.Branch
	if payload.Ref != expectedRef {
		utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "branch not tracked, skipped"})
		return
	}

	// Fetch env vars
	envRows, _ := pool.Query(context.Background(), `SELECT key, value FROM public.deployment_env_vars WHERE app_id = $1`, appID)
	var envVars []EnvVar
	if envRows != nil {
		defer envRows.Close()
		for envRows.Next() {
			var ev EnvVar
			if envRows.Scan(&ev.Key, &ev.Value) == nil {
				envVars = append(envVars, ev)
			}
		}
	}

	// Create build record
	var buildID string
	commitMsg := payload.HeadCommit.Message
	commitSHA := payload.HeadCommit.ID
	if len(commitSHA) > 12 {
		commitSHA = commitSHA[:12]
	}
	err = pool.QueryRow(context.Background(), `
		INSERT INTO public.deployment_builds (app_id, triggered_by, commit_sha, commit_msg, status)
		VALUES ($1, 'webhook', $2, $3, 'pending')
		RETURNING build_id
	`, appID, commitSHA, commitMsg).Scan(&buildID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	go runBuild(buildID, appID, app.RepoURL, app.Branch, commitSHA, app.Region, app.GCPProject, app.CloudRunService, envVars)

	utils.WriteJSON(w, http.StatusAccepted, map[string]string{"build_id": buildID, "status": "pending"})
}
