package cto

import (
	"encoding/json"
	"net/http"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
)

type DBProject struct {
	ID          string    `json:"database_id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	DBType      string    `json:"db_type"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	DBName      string    `json:"db_name"`
	Username    string    `json:"username"`
	Color       string    `json:"color"`
	CreatedBy   *string   `json:"created_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	DBPassword  string    `json:"db_password,omitempty"`
}

type DBProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	DBType      string `json:"db_type"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	DBName      string `json:"db_name"`
	Username    string `json:"username"`
	Color       string `json:"color"`
	DBPassword  string `json:"db_password"`
}

func ListProjectsHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT database_id, workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, created_at, updated_at, COALESCE(db_password, '')
		FROM public.cto_database_projects
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var projects []DBProject
	for rows.Next() {
		var p DBProject
		if err := rows.Scan(
			&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.DBType, &p.Host, &p.Port,
			&p.DBName, &p.Username, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DBPassword,
		); err != nil {
			continue
		}
		projects = append(projects, p)
	}
	if projects == nil {
		projects = []DBProject{}
	}
	utils.WriteJSON(w, http.StatusOK, projects)
}

func CreateProjectHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var input DBProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" {
		utils.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if input.DBType == "" {
		input.DBType = "postgresql"
	}
	if input.Color == "" {
		input.Color = "#3ecf8e"
	}
	if input.Port == 0 {
		input.Port = defaultPort(input.DBType)
	}

	userID, _ := r.Context().Value("user_id").(string)

	var hashedPassword string
	if input.DBPassword != "" {
		hp, err := bcrypt.GenerateFromPassword([]byte(input.DBPassword), bcrypt.DefaultCost)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "failed to hash password: "+err.Error())
			return
		}
		hashedPassword = string(hp)
	}

	var p DBProject
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.cto_database_projects
			(workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, db_password)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, NULLIF($10, ''), $11)
		RETURNING database_id, workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, created_at, updated_at, COALESCE(db_password, '')
	`, workspaceID, input.Name, input.Description, input.DBType, input.Host, input.Port,
		input.DBName, input.Username, input.Color, userID, hashedPassword,
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.DBType, &p.Host, &p.Port,
		&p.DBName, &p.Username, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DBPassword)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	utils.WriteJSON(w, http.StatusCreated, p)
}

func GetProjectHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	workspaceID := r.URL.Query().Get("workspace_id")

	var p DBProject
	var err error
	if workspaceID != "" {
		err = db.GetCTOPoolOrNil().QueryRow(r.Context(), `
			SELECT database_id, workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, created_at, updated_at, COALESCE(db_password, '')
			FROM public.cto_database_projects WHERE database_id = $1 AND workspace_id = $2
		`, id, workspaceID).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.DBType, &p.Host, &p.Port,
			&p.DBName, &p.Username, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DBPassword)
	} else {
		err = db.GetCTOPoolOrNil().QueryRow(r.Context(), `
			SELECT database_id, workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, created_at, updated_at, COALESCE(db_password, '')
			FROM public.cto_database_projects WHERE database_id = $1
		`, id).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.DBType, &p.Host, &p.Port,
			&p.DBName, &p.Username, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DBPassword)
	}
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, p)
}

func UpdateProjectHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var input DBProjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		utils.WriteError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" {
		utils.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}

	var hashedPassword string
	if input.DBPassword != "" {
		hp, err := bcrypt.GenerateFromPassword([]byte(input.DBPassword), bcrypt.DefaultCost)
		if err != nil {
			utils.WriteError(w, http.StatusInternalServerError, "failed to hash password: "+err.Error())
			return
		}
		hashedPassword = string(hp)
	}

	var p DBProject
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		UPDATE public.cto_database_projects
		SET name=$1, description=$2, db_type=$3, host=$4, port=$5, db_name=$6, username=$7, color=$8,
		    db_password = CASE WHEN $9::text <> '' THEN $9::text ELSE db_password END,
		    updated_at=NOW()
		WHERE database_id=$10 AND workspace_id=$11
		RETURNING database_id, workspace_id, name, description, db_type, host, port, db_name, username, color, created_by, created_at, updated_at, COALESCE(db_password, '')
	`, input.Name, input.Description, input.DBType, input.Host, input.Port,
		input.DBName, input.Username, input.Color, hashedPassword, id, workspaceID,
	).Scan(&p.ID, &p.WorkspaceID, &p.Name, &p.Description, &p.DBType, &p.Host, &p.Port,
		&p.DBName, &p.Username, &p.Color, &p.CreatedBy, &p.CreatedAt, &p.UpdatedAt, &p.DBPassword)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, p)
}

func DeleteProjectHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	id := chi.URLParam(r, "id")
	workspaceID := r.URL.Query().Get("workspace_id")

	pool := db.GetCTOPoolOrNil()
	var tag interface{ RowsAffected() int64 }
	var err error
	if workspaceID != "" {
		tag, err = pool.Exec(r.Context(),
			`DELETE FROM public.cto_database_projects WHERE database_id = $1 AND workspace_id = $2`, id, workspaceID)
	} else {
		tag, err = pool.Exec(r.Context(),
			`DELETE FROM public.cto_database_projects WHERE database_id = $1`, id)
	}
	if err != nil || tag.RowsAffected() == 0 {
		utils.WriteError(w, http.StatusNotFound, "project not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "deleted"})
}

func defaultPort(dbType string) int {
	switch dbType {
	case "mysql":
		return 3306
	case "mongodb":
		return 27017
	case "redis":
		return 6379
	case "sqlite":
		return 0
	default:
		return 5432
	}
}
