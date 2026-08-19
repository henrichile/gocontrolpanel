package api

import (
	"net/http"
	"strings"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
	"github.com/etasoft/gocontrolpanel/internal/provision"
)

// --- Bases de datos --------------------------------------------------------

func (s *Server) handleListDatabases(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, accountID); err != nil {
		writeStoreError(w, err)
		return
	}
	dbs, err := s.st.ListDatabases(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"databases": dbs, "host": s.svc.MySQL().Host()})
}

type createDatabaseRequest struct {
	Name     string `json:"name"`     // sufijo; el prefijo es el system_user
	Password string `json:"password"` // opcional: si va vacío se genera
	Charset  string `json:"charset"`
}

func (s *Server) handleCreateDatabase(w http.ResponseWriter, r *http.Request) {
	accountID, err := pathUUID(r, "accountID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de cuenta inválido")
		return
	}
	var req createDatabaseRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	id := auth.MustIdentity(r.Context())
	acct, err := s.authorizeAccount(r.Context(), id, accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if s.svc.MySQL() == nil {
		httpx.Error(w, http.StatusServiceUnavailable,
			"el panel no tiene un servidor MySQL configurado")
		return
	}

	plan, err := s.st.GetPlan(r.Context(), acct.PlanID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	count, err := s.st.CountAccountDatabases(r.Context(), accountID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if count >= plan.MaxDatabases {
		httpx.FieldError(w, "plan", "has alcanzado el límite de bases de datos de tu plan")
		return
	}

	// Prefijo estilo cPanel: <usuario>_<nombre>
	suffix := strings.ToLower(strings.TrimSpace(req.Name))
	dbName := acct.SystemUser + "_" + suffix
	dbUser := acct.SystemUser + "_" + suffix

	password := req.Password
	if password == "" {
		password, err = auth.RandomPassword(24)
		if err != nil {
			httpx.Error(w, http.StatusInternalServerError, "no se pudo generar la contraseña")
			return
		}
	}

	if err := s.svc.MySQL().CreateDatabase(r.Context(), dbName, dbUser, password, req.Charset); err != nil {
		var ve provision.ValidationError
		if asValidation(err, &ve) {
			httpx.FieldError(w, ve.Field, ve.Message)
			return
		}
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}

	record := &models.SiteDatabase{
		AccountID: accountID,
		Engine:    "mysql",
		DBName:    dbName,
		DBUser:    dbUser,
		Charset:   firstNonEmpty(req.Charset, "utf8mb4"),
	}
	if err := s.st.CreateDatabase(r.Context(), record); err != nil {
		// Revertimos la creación en MySQL para no dejar huérfanos.
		_ = s.svc.MySQL().DropDatabase(r.Context(), dbName, dbUser)
		writeStoreError(w, err)
		return
	}

	s.st.Audit(r.Context(), models.AuditEntry{
		ActorID: &id.UserID, ActorUsername: id.Username,
		Action: "database.create", TargetType: "database", TargetID: record.ID.String(),
		Detail: map[string]any{"db_name": dbName}, IPAddress: httpx.ClientIP(r),
	})

	// La contraseña se devuelve una única vez, aquí.
	httpx.Created(w, map[string]any{
		"database": record,
		"password": password,
		"host":     s.svc.MySQL().Host(),
	})
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	dbID, err := pathUUID(r, "databaseID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de base de datos inválido")
		return
	}
	db, err := s.st.GetDatabase(r.Context(), dbID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, err := s.authorizeAccount(r.Context(), id, db.AccountID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.svc.MySQL().DropDatabase(r.Context(), db.DBName, db.DBUser); err != nil {
		httpx.Error(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.st.DeleteDatabase(r.Context(), dbID); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.NoContent(w)
}

// --- Tareas programadas ----------------------------------------------------

func (s *Server) handleListCron(w http.ResponseWriter, r *http.Request) {
	site, _, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	jobs, err := s.st.ListCron(r.Context(), site.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.OK(w, map[string]any{"cron_jobs": jobs})
}

type createCronRequest struct {
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

func (s *Server) handleCreateCron(w http.ResponseWriter, r *http.Request) {
	site, acct, err := s.resolveSite(r)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	var req createCronRequest
	if err := httpx.Decode(w, r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(strings.Fields(req.Schedule)) != 5 {
		httpx.FieldError(w, "schedule", "usa una expresión cron de 5 campos, p.ej. '*/5 * * * *'")
		return
	}
	if strings.TrimSpace(req.Command) == "" {
		httpx.FieldError(w, "command", "es obligatorio")
		return
	}

	plan, err := s.st.GetPlan(r.Context(), acct.PlanID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	existing, err := s.st.ListCron(r.Context(), site.ID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	if len(existing) >= plan.MaxCronJobs {
		httpx.FieldError(w, "plan", "has alcanzado el límite de tareas de tu plan")
		return
	}

	job := &models.CronJob{
		SiteID:   site.ID,
		Schedule: strings.TrimSpace(req.Schedule),
		Command:  strings.TrimSpace(req.Command),
		IsActive: true,
	}
	if err := s.st.CreateCron(r.Context(), job); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.Created(w, map[string]any{"cron_job": job})
}

func (s *Server) handleDeleteCron(w http.ResponseWriter, r *http.Request) {
	cronID, err := pathUUID(r, "cronID")
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "id de tarea inválido")
		return
	}
	job, err := s.st.GetCron(r.Context(), cronID)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	id := auth.MustIdentity(r.Context())
	if _, _, err := s.authorizeSite(r.Context(), id, job.SiteID); err != nil {
		writeStoreError(w, err)
		return
	}
	if err := s.st.DeleteCron(r.Context(), cronID); err != nil {
		writeStoreError(w, err)
		return
	}
	httpx.NoContent(w)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
