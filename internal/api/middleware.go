package api

import (
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	chimw "github.com/go-chi/chi/v5/middleware"

	"github.com/etasoft/gocontrolpanel/internal/auth"
	"github.com/etasoft/gocontrolpanel/internal/httpx"
	"github.com/etasoft/gocontrolpanel/internal/models"
)

// requireAuth valida el JWT de la cabecera Authorization y coloca la
// identidad en el contexto.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := r.Header.Get("Authorization")
		token := ""
		if strings.HasPrefix(header, "Bearer ") {
			token = strings.TrimSpace(header[7:])
		}
		if token == "" {
			// Alternativa para EventSource, que no permite cabeceras propias.
			token = r.URL.Query().Get("access_token")
		}
		if token == "" {
			httpx.Error(w, http.StatusUnauthorized, "falta el token de acceso")
			return
		}

		claims, err := s.tokens.Parse(token)
		if err != nil {
			httpx.Error(w, http.StatusUnauthorized, "token inválido o expirado")
			return
		}

		// Comprobamos que el usuario sigue existiendo y activo.
		user, err := s.st.GetUserByID(r.Context(), claims.UserID)
		if err != nil || !user.IsActive {
			httpx.Error(w, http.StatusUnauthorized, "la sesión ya no es válida")
			return
		}

		ctx := auth.WithIdentity(r.Context(), auth.Identity{
			UserID:   user.ID,
			Username: user.Username,
			Role:     user.Role,
		})
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireRole exige un rol mínimo según la jerarquía admin > reseller > user.
func (s *Server) requireRole(min models.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			id, ok := auth.FromContext(r.Context())
			if !ok || !id.Role.AtLeast(min) {
				httpx.Error(w, http.StatusForbidden, "no tienes permisos para esta operación")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Cross-Origin-Opener-Policy", "same-origin")
		// style-src necesita 'unsafe-inline' porque Monaco inyecta un
		// <style> propio en runtime para los temas de sintaxis; script-src
		// se mantiene estricto a 'self' (sin inline ni eval), que es lo que
		// de verdad frena la inyección clásica de <script>.
		h.Set("Content-Security-Policy", strings.Join([]string{
			"default-src 'self'",
			"script-src 'self'",
			"style-src 'self' 'unsafe-inline'",
			"img-src 'self' data:",
			"font-src 'self' data:",
			"connect-src 'self'",
			"worker-src 'self'",
			"frame-ancestors 'none'",
			"base-uri 'self'",
			"form-action 'self'",
		}, "; "))
		if r.TLS != nil {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		next.ServeHTTP(w, r)
	})
}

// cors solo abre el paso al origen del panel; en desarrollo permite Vite.
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{s.cfg.PublicURL: true}
	if !s.cfg.IsProduction() {
		allowed["http://localhost:5173"] = true
		allowed["http://127.0.0.1:5173"] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			h := w.Header()
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Access-Control-Allow-Credentials", "true")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			h.Set("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := chimw.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)

		lvl := slog.LevelInfo
		if ww.Status() >= 500 {
			lvl = slog.LevelError
		} else if ww.Status() >= 400 {
			lvl = slog.LevelWarn
		}
		slog.Log(r.Context(), lvl, "http",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"ip", httpx.ClientIP(r),
			"request_id", chimw.GetReqID(r.Context()),
		)
	})
}

// --- Rate limiting ---------------------------------------------------------

// rateLimiter es un limitador por IP con ventana deslizante simple, suficiente
// para proteger el endpoint de login de fuerza bruta.
type rateLimiter struct {
	mu       sync.Mutex
	hits     map[string][]time.Time
	limit    int
	window   time.Duration
	lastGC   time.Time
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{
		hits:   make(map[string][]time.Time),
		limit:  limit,
		window: window,
		lastGC: time.Now(),
	}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	if now.Sub(rl.lastGC) > 5*rl.window {
		for k, times := range rl.hits {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(rl.hits, k)
			}
		}
		rl.lastGC = now
	}

	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now)
	return true
}

func (rl *rateLimiter) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !rl.allow(httpx.ClientIP(r)) {
			w.Header().Set("Retry-After", "60")
			httpx.Error(w, http.StatusTooManyRequests,
				"demasiados intentos; espera un minuto antes de reintentar")
			return
		}
		next.ServeHTTP(w, r)
	})
}
