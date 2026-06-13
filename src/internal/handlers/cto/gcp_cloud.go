package cto

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"

	"cto/src/internal/logger"
	"cto/src/internal/utils"
)

type GCPResource struct {
	Name      string         `json:"name"`
	ID        string         `json:"id,omitempty"`
	Zone      string         `json:"zone,omitempty"`
	Region    string         `json:"region,omitempty"`
	Status    string         `json:"status,omitempty"`
	CreatedAt string         `json:"created_at,omitempty"`
	Details   map[string]any `json:"details"`
}

type GCPServiceGroup struct {
	Service   string        `json:"service"`
	Icon      string        `json:"icon"`
	Color     string        `json:"color"`
	Resources []GCPResource `json:"resources"`
	Error     string        `json:"error,omitempty"`
}

type GCPCloudMap struct {
	Project   string            `json:"project"`
	Groups    []GCPServiceGroup `json:"groups"`
	FetchedAt time.Time         `json:"fetched_at"`
}

type gcpFetcher struct {
	service string
	icon    string
	color   string
	fetch   func(ctx context.Context, project string) ([]GCPResource, error)
}

func lastSegment(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) == 0 {
		return s
	}
	return parts[len(parts)-1]
}

func runGcloud(ctx context.Context, args ...string) ([]map[string]any, error) {
	cmd := exec.CommandContext(ctx, "gcloud", args...)
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	var result []map[string]any
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func strField(m map[string]any, key string) string {
	if v, ok := m[key]; ok && v != nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func gcpFetchers() []gcpFetcher {
	return []gcpFetcher{
		{
			service: "Compute Instances",
			icon:    "🖥️",
			color:   "#4285f4",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "instances", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					machineType := ""
					if v := strField(item, "machineType"); v != "" {
						machineType = lastSegment(v)
					}
					networkIP := ""
					if ifaces, ok := item["networkInterfaces"].([]any); ok && len(ifaces) > 0 {
						if iface, ok := ifaces[0].(map[string]any); ok {
							networkIP = strField(iface, "networkIP")
						}
					}
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						ID:     strField(item, "id"),
						Zone:   lastSegment(strField(item, "zone")),
						Status: strField(item, "status"),
						Details: map[string]any{
							"machine_type": machineType,
							"network_ip":   networkIP,
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Cloud SQL",
			icon:    "🗄️",
			color:   "#34a853",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "sql", "instances", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					tier := ""
					if settings, ok := item["settings"].(map[string]any); ok {
						tier = strField(settings, "tier")
					}
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: strField(item, "region"),
						Status: strField(item, "state"),
						Details: map[string]any{
							"database_version": strField(item, "databaseVersion"),
							"tier":             tier,
							"connection_name":  strField(item, "connectionName"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "GKE Clusters",
			icon:    "⎈",
			color:   "#1967d2",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "container", "clusters", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					zone := strField(item, "zone")
					if zone == "" {
						zone = strField(item, "location")
					}
					nodeCount := item["currentNodeCount"]
					k8sVersion := strField(item, "currentMasterVersion")
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Zone:   zone,
						Status: strField(item, "status"),
						Details: map[string]any{
							"node_count":  nodeCount,
							"k8s_version": k8sVersion,
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Cloud Storage Buckets",
			icon:    "🪣",
			color:   "#fbbc04",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "storage", "buckets", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: strField(item, "location"),
						Details: map[string]any{
							"storage_class": strField(item, "storageClass"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Cloud Run Services",
			icon:    "▶️",
			color:   "#ea4335",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "run", "services", "list", "--format=json", "--project="+project, "--platform=managed")
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					name := ""
					region := ""
					if meta, ok := item["metadata"].(map[string]any); ok {
						name = strField(meta, "name")
						if labels, ok := meta["labels"].(map[string]any); ok {
							if loc, ok := labels["cloud.googleapis.com/location"].(string); ok {
								region = loc
							}
						}
					}
					status := ""
					url := ""
					if st, ok := item["status"].(map[string]any); ok {
						url = strField(st, "url")
						if conds, ok := st["conditions"].([]any); ok && len(conds) > 0 {
							if cond, ok := conds[0].(map[string]any); ok {
								status = strField(cond, "status")
							}
						}
					}
					res = append(res, GCPResource{
						Name:   name,
						Region: region,
						Status: status,
						Details: map[string]any{
							"url": url,
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Pub/Sub Topics",
			icon:    "📨",
			color:   "#ff6d00",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "pubsub", "topics", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					fullName := strField(item, "name")
					res = append(res, GCPResource{
						Name: lastSegment(fullName),
						Details: map[string]any{
							"full_name": fullName,
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "VPC Networks",
			icon:    "🌐",
			color:   "#0f9d58",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "networks", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					routingMode := ""
					if rc, ok := item["routingConfig"].(map[string]any); ok {
						routingMode = strField(rc, "routingMode")
					}
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Status: "ACTIVE",
						Details: map[string]any{
							"routing_mode": routingMode,
							"subnet_mode":  strField(item, "x_gcloud_subnet_mode"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Subnets",
			icon:    "🔀",
			color:   "#7248b9",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "networks", "subnets", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: lastSegment(strField(item, "region")),
						Details: map[string]any{
							"network":        lastSegment(strField(item, "network")),
							"ip_range":       strField(item, "ipCidrRange"),
							"private_access": item["privateIpGoogleAccess"],
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Firewall Rules",
			icon:    "🔥",
			color:   "#ff5722",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "firewall-rules", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					status := "ENABLED"
					if disabled, ok := item["disabled"].(bool); ok && disabled {
						status = "DISABLED"
					}
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Status: status,
						Details: map[string]any{
							"direction": strField(item, "direction"),
							"network":   lastSegment(strField(item, "network")),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Cloud Functions",
			icon:    "λ",
			color:   "#00bcd4",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "functions", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					fullName := strField(item, "name")
					// name format: projects/{proj}/locations/{region}/functions/{name}
					nameSeg := lastSegment(fullName)
					region := ""
					parts := strings.Split(fullName, "/")
					if len(parts) >= 4 {
						region = parts[3]
					}
					trigger := "event"
					if ht, ok := item["httpsTrigger"].(map[string]any); ok {
						if url := strField(ht, "url"); url != "" {
							trigger = url
						}
					}
					res = append(res, GCPResource{
						Name:   nameSeg,
						Region: region,
						Status: strField(item, "state"),
						Details: map[string]any{
							"runtime": strField(item, "runtime"),
							"trigger": trigger,
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "IAM Service Accounts",
			icon:    "🔑",
			color:   "#607d8b",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "iam", "service-accounts", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					name := strField(item, "displayName")
					if name == "" {
						name = strField(item, "email")
					}
					status := "ACTIVE"
					if disabled, ok := item["disabled"].(bool); ok && disabled {
						status = "DISABLED"
					}
					res = append(res, GCPResource{
						Name:   name,
						Status: status,
						Details: map[string]any{
							"email": strField(item, "email"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Artifact Registry",
			icon:    "📦",
			color:   "#795548",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "artifacts", "repositories", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					res = append(res, GCPResource{
						Name:   lastSegment(strField(item, "name")),
						Region: strField(item, "location"),
						Details: map[string]any{
							"format": strField(item, "format"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Memorystore (Redis)",
			icon:    "⚡",
			color:   "#d32f2f",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "redis", "instances", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: strField(item, "locationId"),
						Status: strField(item, "state"),
						Details: map[string]any{
							"tier":      strField(item, "tier"),
							"memory_gb": item["memorySizeGb"],
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Forwarding Rules (LB)",
			icon:    "⚖️",
			color:   "#1565c0",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "forwarding-rules", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					region := lastSegment(strField(item, "region"))
					if region == "" {
						region = "global"
					}
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: region,
						Details: map[string]any{
							"ip":         strField(item, "IPAddress"),
							"port_range": strField(item, "portRange"),
							"target":     strField(item, "target"),
						},
					})
				}
				return res, nil
			},
		},
		{
			service: "Cloud Routers",
			icon:    "🔄",
			color:   "#388e3c",
			fetch: func(ctx context.Context, project string) ([]GCPResource, error) {
				items, err := runGcloud(ctx, "compute", "routers", "list", "--format=json", "--project="+project)
				if err != nil {
					return nil, err
				}
				res := make([]GCPResource, 0, len(items))
				for _, item := range items {
					res = append(res, GCPResource{
						Name:   strField(item, "name"),
						Region: lastSegment(strField(item, "region")),
						Details: map[string]any{
							"network": lastSegment(strField(item, "network")),
						},
					})
				}
				return res, nil
			},
		},
	}
}

func GetGCPCloudMapHandler(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = "bhanshu"
	}

	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	fetchers := gcpFetchers()
	groups := make([]GCPServiceGroup, len(fetchers))

	var wg sync.WaitGroup
	for i, f := range fetchers {
		wg.Add(1)
		go func(idx int, fetcher gcpFetcher) {
			defer wg.Done()
			resources, err := fetcher.fetch(ctx, project)
			group := GCPServiceGroup{
				Service: fetcher.service,
				Icon:    fetcher.icon,
				Color:   fetcher.color,
			}
			if err != nil {
				logger.LogHandler("GCP fetch error for %s: %v", fetcher.service, err)
				group.Error = err.Error()
				group.Resources = []GCPResource{}
			} else {
				group.Resources = resources
			}
			groups[idx] = group
		}(i, f)
	}
	wg.Wait()

	utils.WriteJSON(w, http.StatusOK, GCPCloudMap{
		Project:   project,
		Groups:    groups,
		FetchedAt: time.Now(),
	})
}

func ListGCPProjectsHandler(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "gcloud", "projects", "list", "--format=json")
	out, err := cmd.Output()
	if err != nil {
		logger.LogHandler("gcloud projects list error: %v", err)
		utils.WriteError(w, http.StatusInternalServerError, "failed to list GCP projects: "+err.Error())
		return
	}

	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "failed to parse gcloud output: "+err.Error())
		return
	}

	type ProjectInfo struct {
		ProjectID      string `json:"project_id"`
		Name           string `json:"name"`
		ProjectNumber  string `json:"project_number"`
		LifecycleState string `json:"lifecycle_state"`
	}

	projects := make([]ProjectInfo, 0, len(raw))
	for _, item := range raw {
		projects = append(projects, ProjectInfo{
			ProjectID:      strField(item, "projectId"),
			Name:           strField(item, "name"),
			ProjectNumber:  strField(item, "projectNumber"),
			LifecycleState: strField(item, "lifecycleState"),
		})
	}

	utils.WriteJSON(w, http.StatusOK, projects)
}

type SQLTier struct {
	Tier      string  `json:"tier"`
	RAMGB     float64 `json:"ram_gb"`
	DiskQuota int64   `json:"disk_quota_gb"`
}

func GetSQLTiersHandler(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = gcpProject()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gcloud", "sql", "tiers", "list", "--format=json", "--project="+project).Output()
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "failed to list SQL tiers: "+err.Error())
		return
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "failed to parse tiers: "+err.Error())
		return
	}

	tiers := make([]SQLTier, 0, len(raw))
	for _, item := range raw {
		tier := strField(item, "tier")
		if tier == "" {
			tier = strField(item, "Tier")
		}
		if tier == "" {
			continue
		}
		var ramGB float64
		switch v := item["RAM"].(type) {
		case float64:
			ramGB = v / (1024 * 1024 * 1024)
		case string:
			var n int64
			fmt.Sscanf(v, "%d", &n)
			ramGB = float64(n) / (1024 * 1024 * 1024)
		}
		var diskGB int64
		switch v := item["DiskQuota"].(type) {
		case float64:
			diskGB = int64(v) / (1024 * 1024 * 1024)
		case string:
			fmt.Sscanf(v, "%d", &diskGB)
			diskGB /= (1024 * 1024 * 1024)
		}
		tiers = append(tiers, SQLTier{Tier: tier, RAMGB: ramGB, DiskQuota: diskGB})
	}
	utils.WriteJSON(w, http.StatusOK, tiers)
}

func GetComputeRegionsHandler(w http.ResponseWriter, r *http.Request) {
	project := r.URL.Query().Get("project")
	if project == "" {
		project = gcpProject()
	}
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "gcloud", "compute", "regions", "list", "--format=json", "--project="+project).Output()
	if err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "failed to list regions: "+err.Error())
		return
	}
	var raw []map[string]any
	if err := json.Unmarshal(out, &raw); err != nil {
		utils.WriteError(w, http.StatusInternalServerError, "failed to parse regions: "+err.Error())
		return
	}

	type Region struct {
		Name   string `json:"name"`
		Status string `json:"status"`
	}
	regions := make([]Region, 0, len(raw))
	for _, item := range raw {
		regions = append(regions, Region{
			Name:   strField(item, "name"),
			Status: strField(item, "status"),
		})
	}
	utils.WriteJSON(w, http.StatusOK, regions)
}
