package web

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cheapspace/internal/config"
	"cheapspace/internal/db"
	"cheapspace/internal/service"
	webui "cheapspace/web"
)

type Server struct {
	cfg       config.Config
	service   *service.Service
	templates *template.Template
}

type viewData struct {
	Title                    string
	Active                   string
	Error                    string
	Notice                   string
	Config                   config.Config
	SecretFingerprint        string
	ShowDeleted              bool
	Form                     workspaceForm
	Workspaces               []db.Workspace
	WorkspaceDetails         service.WorkspaceDetails
	Jobs                     []db.Job
	JobDetails               service.JobDetails
	LastLogSequence          int
	InitialPasswordAvailable bool
}

type workspaceForm struct {
	Name                string
	RepoURL             string
	DotfilesURL         string
	SourceType          string
	SourceRef           string
	SSHKeys             string
	SSHPort             int
	PasswordAuthEnabled bool
	HTTPProxy           string
	HTTPSProxy          string
	NoProxy             string
	ProxyPACURL         string
	CPUMillis           int
	MemoryMB            int
	TTLMinutes          int
	TraefikEnabled      bool
	TraefikBaseDomain   string
}

type jobAPIResponse struct {
	Job  db.Job      `json:"job"`
	Logs []db.JobLog `json:"logs"`
}

type workspaceAPIResponse struct {
	Workspace db.Workspace        `json:"workspace"`
	Events    []db.WorkspaceEvent `json:"events"`
	Jobs      []db.Job            `json:"jobs"`
}

func New(cfg config.Config, svc *service.Service) (*Server, error) {
	funcs := template.FuncMap{
		"formatTime": func(t time.Time) string {
			if t.IsZero() {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"formatOptTime": func(t *time.Time) string {
			if t == nil || t.IsZero() {
				return "-"
			}
			return t.Local().Format("2006-01-02 15:04:05")
		},
		"statusClass": statusClass,
		"sourceLabel": sourceLabel,
		"truncate": func(value string, max int) string {
			if max <= 0 || len(value) <= max {
				return value
			}
			return value[:max] + "..."
		},
		"joinSSHKeys": func(keys []db.WorkspaceSSHKey) string {
			items := make([]string, 0, len(keys))
			for _, key := range keys {
				items = append(items, key.PublicKey)
			}
			return strings.Join(items, "\n")
		},
	}

	templates, err := template.New("root").Funcs(funcs).ParseFS(webui.Templates(), "*.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	return &Server{
		cfg:       cfg,
		service:   svc,
		templates: templates,
	}, nil
}

func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /static/", http.StripPrefix("/static/", http.FileServerFS(webui.Static())))
	mux.HandleFunc("GET /", s.handleHome)
	mux.HandleFunc("GET /healthz", s.handleHealthz)
	mux.HandleFunc("GET /workspaces", s.handleWorkspaces)
	mux.HandleFunc("GET /workspaces/new", s.handleWorkspaceNew)
	mux.HandleFunc("POST /workspaces", s.handleWorkspaceCreate)
	mux.HandleFunc("GET /workspaces/{id}", s.handleWorkspaceDetail)
	mux.HandleFunc("POST /workspaces/{id}/delete", s.handleWorkspaceDelete)
	mux.HandleFunc("POST /workspaces/{id}/password/reveal", s.handleWorkspacePasswordReveal)
	mux.HandleFunc("GET /api/workspaces/{id}", s.handleWorkspaceAPI)
	mux.HandleFunc("GET /jobs", s.handleJobs)
	mux.HandleFunc("GET /jobs/{id}", s.handleJobDetail)
	mux.HandleFunc("POST /jobs/{id}/cancel", s.handleJobCancel)
	mux.HandleFunc("POST /jobs/{id}/resume", s.handleJobResume)
	mux.HandleFunc("POST /jobs/{id}/delete", s.handleJobDelete)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleJobAPI)
	return mux
}

func (s *Server) handleHome(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/workspaces", http.StatusSeeOther)
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":                "ok",
		"runtime":               s.cfg.Runtime,
		"secretFingerprint":     s.cfg.SecretFingerprint(),
		"defaultWorkspaceImage": s.cfg.DefaultWorkspaceImage,
	})
}

func (s *Server) handleWorkspaces(w http.ResponseWriter, r *http.Request) {
	showDeleted := queryEnabled(r.URL.Query().Get("show_deleted"))
	workspaces, err := s.service.ListWorkspaces(r.Context(), showDeleted)
	if err != nil {
		s.renderError(w, err, http.StatusInternalServerError)
		return
	}
	s.render(w, "workspaces", viewData{
		Title:             "Workspaces",
		Active:            "workspaces",
		Config:            s.cfg,
		SecretFingerprint: s.cfg.SecretFingerprint(),
		ShowDeleted:       showDeleted,
		Workspaces:        workspaces,
	})
}

func (s *Server) handleWorkspaceNew(w http.ResponseWriter, r *http.Request) {
	s.render(w, "workspace_new", viewData{
		Title:             "Create workspace",
		Active:            "workspaces",
		Config:            s.cfg,
		SecretFingerprint: s.cfg.SecretFingerprint(),
		Form: workspaceForm{
			SourceType: "builtin_image",
			NoProxy:    defaultNoProxyValue(),
			CPUMillis:  minInt(2000, s.cfg.MaxCPUMillis),
			MemoryMB:   minInt(4096, s.cfg.MaxMemoryMB),
			TTLMinutes: minInt(480, s.cfg.MaxTTLMinutes),
		},
	})
}

func (s *Server) handleWorkspaceCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		s.renderError(w, err, http.StatusBadRequest)
		return
	}

	form := workspaceForm{
		Name:              strings.TrimSpace(r.FormValue("name")),
		RepoURL:           strings.TrimSpace(r.FormValue("repo_url")),
		DotfilesURL:       strings.TrimSpace(r.FormValue("dotfiles_url")),
		SourceType:        strings.TrimSpace(r.FormValue("source_type")),
		SourceRef:         r.FormValue("source_ref"),
		SSHKeys:           r.FormValue("ssh_keys"),
		SSHPort:           atoiDefault(r.FormValue("ssh_port"), 0),
		HTTPProxy:         strings.TrimSpace(r.FormValue("http_proxy")),
		HTTPSProxy:        strings.TrimSpace(r.FormValue("https_proxy")),
		NoProxy:           strings.TrimSpace(r.FormValue("no_proxy")),
		ProxyPACURL:       strings.TrimSpace(r.FormValue("proxy_pac_url")),
		CPUMillis:         atoiDefault(r.FormValue("cpu_millis"), minInt(2000, s.cfg.MaxCPUMillis)),
		MemoryMB:          atoiDefault(r.FormValue("memory_mb"), minInt(4096, s.cfg.MaxMemoryMB)),
		TTLMinutes:        atoiDefault(r.FormValue("ttl_minutes"), minInt(480, s.cfg.MaxTTLMinutes)),
		TraefikEnabled:    r.FormValue("traefik_enabled") == "on",
		TraefikBaseDomain: strings.TrimSpace(r.FormValue("traefik_base_domain")),
	}
	if form.SourceType == "builtin_image" {
		form.SourceRef = ""
	}
	if form.HTTPProxy != "" && form.HTTPSProxy == "" {
		form.HTTPSProxy = form.HTTPProxy
	}

	workspace, err := s.service.CreateWorkspace(r.Context(), service.CreateWorkspaceInput{
		Name:              form.Name,
		RepoURL:           form.RepoURL,
		DotfilesURL:       form.DotfilesURL,
		SourceType:        form.SourceType,
		SourceRef:         form.SourceRef,
		SSHKeys:           []string{form.SSHKeys},
		SSHPort:           form.SSHPort,
		HTTPProxy:         form.HTTPProxy,
		HTTPSProxy:        form.HTTPSProxy,
		NoProxy:           form.NoProxy,
		ProxyPACURL:       form.ProxyPACURL,
		CPUMillis:         form.CPUMillis,
		MemoryMB:          form.MemoryMB,
		TTLMinutes:        form.TTLMinutes,
		TraefikEnabled:    form.TraefikEnabled,
		TraefikBaseDomain: form.TraefikBaseDomain,
	})
	if err != nil {
		s.render(w, "workspace_new", viewData{
			Title:             "Create workspace",
			Active:            "workspaces",
			Config:            s.cfg,
			SecretFingerprint: s.cfg.SecretFingerprint(),
			Form:              form,
			Error:             err.Error(),
		})
		return
	}

	http.Redirect(w, r, "/workspaces/"+workspace.ID, http.StatusSeeOther)
}

func (s *Server) handleWorkspaceDetail(w http.ResponseWriter, r *http.Request) {
	workspaceID := r.PathValue("id")
	details, err := s.service.GetWorkspaceDetails(r.Context(), workspaceID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, err, http.StatusInternalServerError)
		return
	}

	s.render(w, "workspace_detail", viewData{
		Title:                    details.Workspace.Name,
		Active:                   "workspaces",
		Config:                   s.cfg,
		SecretFingerprint:        s.cfg.SecretFingerprint(),
		WorkspaceDetails:         details,
		InitialPasswordAvailable: details.Workspace.PasswordAuthEnabled,
	})
}

func (s *Server) handleWorkspaceDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.service.QueueWorkspaceDelete(r.Context(), r.PathValue("id")); err != nil {
		s.renderError(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/workspaces/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleWorkspacePasswordReveal(w http.ResponseWriter, r *http.Request) {
	password, err := s.service.RevealPassword(r.Context(), r.PathValue("id"))
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"password": password})
}

func (s *Server) handleWorkspaceAPI(w http.ResponseWriter, r *http.Request) {
	details, err := s.service.GetWorkspaceDetails(r.Context(), r.PathValue("id"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, workspaceAPIResponse{
		Workspace: details.Workspace,
		Events:    details.Events,
		Jobs:      details.Jobs,
	})
}

func (s *Server) handleJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := s.service.ListJobs(r.Context())
	if err != nil {
		s.renderError(w, err, http.StatusInternalServerError)
		return
	}
	s.render(w, "jobs", viewData{
		Title:             "Jobs",
		Active:            "jobs",
		Config:            s.cfg,
		SecretFingerprint: s.cfg.SecretFingerprint(),
		Jobs:              jobs,
	})
}

func (s *Server) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	details, err := s.service.GetJobDetails(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.renderError(w, err, http.StatusInternalServerError)
		return
	}
	lastSequence := 0
	if len(details.Logs) > 0 {
		lastSequence = details.Logs[len(details.Logs)-1].Sequence
	}
	s.render(w, "job_detail", viewData{
		Title:             details.Job.JobType,
		Active:            "jobs",
		Config:            s.cfg,
		SecretFingerprint: s.cfg.SecretFingerprint(),
		JobDetails:        details,
		LastLogSequence:   lastSequence,
	})
}

func (s *Server) handleJobCancel(w http.ResponseWriter, r *http.Request) {
	if err := s.service.RequestJobCancel(r.Context(), r.PathValue("id")); err != nil {
		s.renderError(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/jobs/"+r.PathValue("id"), http.StatusSeeOther)
}

func (s *Server) handleJobResume(w http.ResponseWriter, r *http.Request) {
	if err := s.service.ResumeJob(r.Context(), r.PathValue("id")); err != nil {
		s.renderError(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/jobs", http.StatusSeeOther)
}

func (s *Server) handleJobDelete(w http.ResponseWriter, r *http.Request) {
	if err := s.service.DeleteJob(r.Context(), r.PathValue("id")); err != nil {
		s.renderError(w, err, http.StatusBadRequest)
		return
	}
	http.Redirect(w, r, "/jobs", http.StatusSeeOther)
}

func (s *Server) handleJobAPI(w http.ResponseWriter, r *http.Request) {
	afterSequence := atoiDefault(r.URL.Query().Get("after"), 0)
	details, err := s.service.GetJobDetails(r.Context(), r.PathValue("id"), afterSequence)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, jobAPIResponse{
		Job:  details.Job,
		Logs: details.Logs,
	})
}

func (s *Server) render(w http.ResponseWriter, templateName string, data viewData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.templates.ExecuteTemplate(w, templateName, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) renderError(w http.ResponseWriter, err error, statusCode int) {
	http.Error(w, err.Error(), statusCode)
}

func writeJSON(w http.ResponseWriter, statusCode int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(value)
}

func statusClass(value string) string {
	switch value {
	case "running", "done":
		return "bg-emerald-500/15 text-emerald-300 ring-1 ring-inset ring-emerald-400/40"
	case "queued", "pending", "building", "provisioning":
		return "bg-sky-500/15 text-sky-300 ring-1 ring-inset ring-sky-400/40"
	case "deleting":
		return "bg-amber-500/15 text-amber-300 ring-1 ring-inset ring-amber-400/40"
	case "failed", "cancelled":
		return "bg-rose-500/15 text-rose-300 ring-1 ring-inset ring-rose-400/40"
	default:
		return "bg-slate-500/15 text-slate-300 ring-1 ring-inset ring-slate-400/40"
	}
}

func sourceLabel(value string) string {
	switch value {
	case "builtin_image":
		return "Built-in image"
	case "image_ref":
		return "Image ref"
	case "dockerfile":
		return "Dockerfile"
	case "nixpacks":
		return "Nixpacks"
	default:
		return value
	}
}

func atoiDefault(value string, fallback int) int {
	if value == "" {
		return fallback
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func queryEnabled(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func defaultNoProxyValue() string {
	return "localhost,127.0.0.1,::1,host.docker.internal"
}

func ServerContext(ctx context.Context, handler http.Handler) error {
	server := &http.Server{
		Addr:    ":0",
		Handler: handler,
	}
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	return server.ListenAndServe()
}
