package cto

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"cto/src/internal/db"
	"cto/src/internal/logger"
	"cto/src/internal/utils"

	"github.com/go-chi/chi/v5"
)

type DBaaSInstance struct {
	InstanceID      string    `json:"instance_id"`
	WorkspaceID     string    `json:"workspace_id"`
	CreatedBy       *string   `json:"created_by,omitempty"`
	Name            string    `json:"name"`
	GCPInstanceName string    `json:"gcp_instance_name"`
	GCPProject      string    `json:"gcp_project"`
	Region          string    `json:"region"`
	DBType          string    `json:"db_type"`
	Tier            string    `json:"tier"`
	Status          string    `json:"status"`
	ConnectionName  string    `json:"connection_name"`
	Host            string    `json:"host"`
	RootPassword    string    `json:"root_password,omitempty"`
	FailedReason    string    `json:"failed_reason,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type CreateDBaaSInput struct {
	Name              string `json:"name"`
	DBType            string `json:"db_type"`
	DBVersion         string `json:"db_version"`
	Region            string `json:"region"`
	Tier              string `json:"tier"`
	StorageGB         int    `json:"storage_gb"`
	Availability      string `json:"availability"`
	BackupEnabled     bool   `json:"backup_enabled"`
	DeletionProtect   bool   `json:"deletion_protection"`
}

func gcpProject() string {
	if p := os.Getenv("GCP_PROJECT_ID"); p != "" {
		return p
	}
	return "bhanshu"
}

func dbaasDBVersion(dbType, explicit string) string {
	if explicit != "" {
		return explicit
	}
	if dbType == "mysql" || dbType == "mysql57" {
		return "MYSQL_8_0"
	}
	return "POSTGRES_15"
}

func dbaasRootUser(dbVersion string) string {
	if strings.HasPrefix(dbVersion, "MYSQL") {
		return "root"
	}
	return "postgres"
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func ListDBaaSInstancesHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	rows, err := db.GetCTOPoolOrNil().Query(r.Context(), `
		SELECT instance_id, workspace_id, created_by, name, gcp_instance_name, gcp_project,
		       region, db_type, tier, status, connection_name, host, root_password, failed_reason, created_at, updated_at
		FROM public.dbaas_instances
		WHERE workspace_id = $1
		ORDER BY created_at DESC
	`, workspaceID)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer rows.Close()

	var instances []DBaaSInstance
	for rows.Next() {
		var inst DBaaSInstance
		if err := rows.Scan(
			&inst.InstanceID, &inst.WorkspaceID, &inst.CreatedBy, &inst.Name,
			&inst.GCPInstanceName, &inst.GCPProject, &inst.Region, &inst.DBType,
			&inst.Tier, &inst.Status, &inst.ConnectionName, &inst.Host,
			&inst.RootPassword, &inst.FailedReason, &inst.CreatedAt, &inst.UpdatedAt,
		); err != nil {
			continue
		}
		instances = append(instances, inst)
	}
	if instances == nil {
		instances = []DBaaSInstance{}
	}
	utils.WriteJSON(w, http.StatusOK, instances)
}

func CreateDBaaSInstanceHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	workspaceID := r.URL.Query().Get("workspace_id")
	if workspaceID == "" {
		utils.WriteError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}

	var input CreateDBaaSInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil || input.Name == "" {
		utils.WriteError(w, http.StatusBadRequest, "name is required")
		return
	}
	if input.DBType == "" {
		input.DBType = "postgres"
	}
	if input.Region == "" {
		input.Region = "us-central1"
	}
	if input.Tier == "" {
		input.Tier = "db-f1-micro"
	}
	if input.StorageGB <= 0 {
		input.StorageGB = 20
	}
	if input.Availability == "" {
		input.Availability = "ZONAL"
	}
	dbVersion := dbaasDBVersion(input.DBType, input.DBVersion)

	project := gcpProject()
	slug := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(input.Name, " ", "-"), "_", "-"))
	gcpName := "zef-" + slug + "-" + randomHex(4)
	rootPwd := randomHex(12)
	userID, _ := r.Context().Value("user_id").(string)

	var inst DBaaSInstance
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		INSERT INTO public.dbaas_instances
			(workspace_id, created_by, name, gcp_instance_name, gcp_project, region, db_type, tier, status, root_password)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, 'provisioning', $9)
		RETURNING instance_id, workspace_id, created_by, name, gcp_instance_name, gcp_project,
		          region, db_type, tier, status, connection_name, host, root_password, failed_reason, created_at, updated_at
	`, workspaceID, userID, input.Name, gcpName, project, input.Region, input.DBType, input.Tier, rootPwd,
	).Scan(
		&inst.InstanceID, &inst.WorkspaceID, &inst.CreatedBy, &inst.Name,
		&inst.GCPInstanceName, &inst.GCPProject, &inst.Region, &inst.DBType,
		&inst.Tier, &inst.Status, &inst.ConnectionName, &inst.Host,
		&inst.RootPassword, &inst.FailedReason, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}

	go provisionCloudSQL(inst.InstanceID, inst.WorkspaceID, userID, inst.Name, gcpName, project, input.Region, dbVersion, input.Tier,
		input.StorageGB, input.Availability, input.BackupEnabled, input.DeletionProtect, rootPwd)

	utils.WriteJSON(w, http.StatusCreated, inst)
}

func provisionCloudSQL(instanceID, workspaceID, createdBy, instName, gcpName, project, region, dbVersion, tier string, storageGB int, availability string, backupEnabled, deletionProtect bool, rootPwd string) {
	logger.LogHandler("DBaaS: provisioning %s in project %s region %s tier %s", gcpName, project, region, tier)

	// Legacy shared-core and standard tiers (db-f1-micro, db-g1-small, db-n1-*) require
	// ENTERPRISE edition; ENTERPRISE_PLUS only accepts db-perf-optimized-N-* tiers.
	edition := "ENTERPRISE_PLUS"
	if strings.HasPrefix(tier, "db-f1-") || strings.HasPrefix(tier, "db-g1-") || strings.HasPrefix(tier, "db-n1-") {
		edition = "ENTERPRISE"
	}

	args := []string{
		"sql", "instances", "create", gcpName,
		"--database-version=" + dbVersion,
		"--tier=" + tier,
		"--edition=" + edition,
		"--region=" + region,
		"--storage-size=" + fmt.Sprintf("%dGB", storageGB),
		"--storage-type=SSD",
		"--project=" + project,
		"--quiet",
	}
	if availability == "REGIONAL" {
		args = append(args, "--availability-type=REGIONAL")
	} else {
		args = append(args, "--availability-type=ZONAL")
	}
	if backupEnabled {
		args = append(args, "--backup")
	} else {
		args = append(args, "--no-backup")
	}
	if deletionProtect {
		args = append(args, "--deletion-protection")
	}
	out, err := exec.Command("gcloud", args...).CombinedOutput()

	pool := db.GetCTOPoolOrNil()
	if pool == nil {
		return
	}

	if err != nil {
		reason := fmt.Sprintf("%v — %s", err, strings.TrimSpace(string(out)))
		logger.LogHandler("DBaaS: provisioning failed for %s: %s", gcpName, reason)
		pool.Exec(context.Background(), `
			UPDATE public.dbaas_instances SET status='failed', failed_reason=$2, updated_at=NOW() WHERE instance_id=$1
		`, instanceID, reason)
		return
	}

	// Set root password — infer engine from version string
	rootUser := dbaasRootUser(dbVersion)
	setPassOut, setPassErr := exec.Command("gcloud", "sql", "users", "set-password", rootUser,
		"--instance="+gcpName, "--password="+rootPwd, "--project="+project, "--quiet",
	).CombinedOutput()
	if setPassErr != nil {
		logger.LogHandler("DBaaS: set-password warning for %s: %v — %s", gcpName, setPassErr, string(setPassOut))
	}

	// Describe to get connection name and public IP
	descOut, descErr := exec.Command("gcloud", "sql", "instances", "describe", gcpName,
		"--project="+project, "--format=json",
	).Output()

	connectionName := ""
	host := ""
	if descErr == nil {
		var desc map[string]any
		if json.Unmarshal(descOut, &desc) == nil {
			if cn, ok := desc["connectionName"].(string); ok {
				connectionName = cn
			}
			if ipAddrs, ok := desc["ipAddresses"].([]any); ok {
				for _, entry := range ipAddrs {
					if m, ok := entry.(map[string]any); ok {
						if m["type"] == "PRIMARY" {
							host, _ = m["ipAddress"].(string)
							break
						}
					}
				}
			}
		}
	}

	pool.Exec(context.Background(), `
		UPDATE public.dbaas_instances
		SET status='ready', connection_name=$1, host=$2, updated_at=NOW()
		WHERE instance_id=$3
	`, connectionName, host, instanceID)

	// Auto-register as a connected database project so SQL editor works on it
	dbType := "postgresql"
	port := 5432
	dbName := "postgres"
	username := "postgres"
	if strings.HasPrefix(dbVersion, "MYSQL") {
		dbType = "mysql"
		port = 3306
		dbName = "mysql"
		username = "root"
	}
	pool.Exec(context.Background(), `
		INSERT INTO public.cto_database_projects
			(workspace_id, created_by, name, db_type, host, port, db_name, username, db_password, dbaas_instance_id, color)
		VALUES ($1, NULLIF($2,'')::uuid, $3, $4, $5, $6, $7, $8, $9, $10::uuid, '#4285f4')
		ON CONFLICT DO NOTHING
	`, workspaceID, createdBy, instName, dbType, host, port, dbName, username, rootPwd, instanceID)

	logger.LogHandler("DBaaS: instance %s ready (connection: %s, host: %s)", gcpName, connectionName, host)
}

func GetDBaaSInstanceHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	instanceID := chi.URLParam(r, "instanceId")

	var inst DBaaSInstance
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		SELECT instance_id, workspace_id, created_by, name, gcp_instance_name, gcp_project,
		       region, db_type, tier, status, connection_name, host, root_password, failed_reason, created_at, updated_at
		FROM public.dbaas_instances WHERE instance_id = $1
	`, instanceID).Scan(
		&inst.InstanceID, &inst.WorkspaceID, &inst.CreatedBy, &inst.Name,
		&inst.GCPInstanceName, &inst.GCPProject, &inst.Region, &inst.DBType,
		&inst.Tier, &inst.Status, &inst.ConnectionName, &inst.Host,
		&inst.RootPassword, &inst.FailedReason, &inst.CreatedAt, &inst.UpdatedAt,
	)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "instance not found")
		return
	}
	utils.WriteJSON(w, http.StatusOK, inst)
}

func DeleteDBaaSInstanceHandler(w http.ResponseWriter, r *http.Request) {
	if !ctoDBRequired(w) {
		return
	}
	instanceID := chi.URLParam(r, "instanceId")

	var gcpName, project string
	err := db.GetCTOPoolOrNil().QueryRow(r.Context(), `
		UPDATE public.dbaas_instances SET status='deleting', updated_at=NOW()
		WHERE instance_id=$1
		RETURNING gcp_instance_name, gcp_project
	`, instanceID).Scan(&gcpName, &project)
	if err != nil {
		utils.WriteError(w, http.StatusNotFound, "instance not found")
		return
	}

	go deleteCloudSQL(instanceID, gcpName, project)

	utils.WriteJSON(w, http.StatusOK, map[string]string{"message": "deletion initiated"})
}

func deleteCloudSQL(instanceID, gcpName, project string) {
	logger.LogHandler("DBaaS: deleting %s in project %s", gcpName, project)
	out, err := exec.Command("gcloud", "sql", "instances", "delete", gcpName,
		"--project="+project, "--quiet",
	).CombinedOutput()
	if err != nil {
		logger.LogHandler("DBaaS: delete failed for %s: %v — %s", gcpName, err, string(out))
	}
	pool := db.GetCTOPoolOrNil()
	if pool != nil {
		pool.Exec(context.Background(), `DELETE FROM public.cto_database_projects WHERE dbaas_instance_id=$1::uuid`, instanceID)
		pool.Exec(context.Background(), `DELETE FROM public.dbaas_instances WHERE instance_id=$1`, instanceID)
	}
}
