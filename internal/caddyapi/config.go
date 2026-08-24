package caddyapi

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Modelo mínimo de la configuración JSON de Caddy: solo las partes que el
// panel gestiona (servidor HTTP de borde + automatización TLS).

type Config struct {
	Admin   *Admin `json:"admin,omitempty"`
	Logging *struct {
		Logs map[string]LogConfig `json:"logs,omitempty"`
	} `json:"logging,omitempty"`
	Apps Apps `json:"apps"`
}

type Admin struct {
	Listen   string `json:"listen,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type LogConfig struct {
	Level string `json:"level,omitempty"`
}

type Apps struct {
	HTTP *HTTPApp `json:"http,omitempty"`
	TLS  *TLSApp  `json:"tls,omitempty"`
}

type HTTPApp struct {
	Servers map[string]*Server `json:"servers"`
}

type Server struct {
	Listen    []string `json:"listen"`
	Routes    []Route  `json:"routes"`
	Logs      *Logs    `json:"logs,omitempty"`
	AutoHTTPS *struct {
		Disable bool `json:"disable,omitempty"`
	} `json:"automatic_https,omitempty"`
}

type Logs struct {
	DefaultLoggerName string            `json:"default_logger_name,omitempty"`
	LoggerNames       map[string]string `json:"logger_names,omitempty"`
}

type Route struct {
	Match    []Match           `json:"match,omitempty"`
	Handle   []json.RawMessage `json:"handle,omitempty"`
	Terminal bool              `json:"terminal,omitempty"`
}

type Match struct {
	Host []string `json:"host,omitempty"`
	Path []string `json:"path,omitempty"`
}

type TLSApp struct {
	// Certificates.Automate fuerza la obtención/renovación de certificados
	// para hosts que no tienen ninguna ruta HTTP propia (como el hostname del
	// mailserver): sin esto, Caddy solo gestiona proactivamente los nombres
	// que aparecen en el "host" de alguna ruta de http.servers — estar
	// listado en automation.policies[].subjects no alcanza, eso solo define
	// QUÉ política/emisor usar SI el nombre llega a necesitar un certificado,
	// no que lo pida de entrada.
	Certificates *TLSCertificates `json:"certificates,omitempty"`
	Automation   *TLSAutomation   `json:"automation,omitempty"`
}

type TLSCertificates struct {
	Automate []string `json:"automate,omitempty"`
}

type TLSAutomation struct {
	Policies []TLSPolicy `json:"policies,omitempty"`
	OnDemand *OnDemand   `json:"on_demand,omitempty"`
}

type TLSPolicy struct {
	Subjects []string          `json:"subjects,omitempty"`
	Issuers  []json.RawMessage `json:"issuers,omitempty"`
	OnDemand bool              `json:"on_demand,omitempty"`
}

type OnDemand struct {
	Permission json.RawMessage `json:"permission,omitempty"`
}

// --- Constructor -----------------------------------------------------------

// SiteRoute describe un vhost a publicar en el borde.
type SiteRoute struct {
	Hosts      []string // dominios que apuntan a este sitio
	Upstream   string   // host:puerto del contenedor FrankenPHP
	RedirectTo string   // si no está vacío, se emite un 301 en lugar de proxy
	ForceHTTPS bool
	Offline    bool // el contenedor no está corriendo: se sirve una página de aviso
}

// BuildOptions parametriza la generación de la configuración de borde.
type BuildOptions struct {
	// Correo para ACME. Vacío = certificados internos (útil en desarrollo).
	ACMEEmail string
	// Endpoint del panel que autoriza la emisión on-demand de certificados.
	// Caddy lo consulta antes de pedir un certificado para un dominio nuevo.
	OnDemandAskURL string
	// Nivel de log de acceso ("INFO", "ERROR"…).
	LogLevel string
	// Puertos de escucha del borde.
	Listen []string
	// Dominio del propio panel; se enruta al backend Go.
	PanelHost     string
	PanelUpstream string
	// Webmail (Roundcube), si el correo gestionado está habilitado. Mismo
	// tratamiento que PanelHost: ruta fija + certificado ACME explícito, no
	// on-demand.
	WebmailHost     string
	WebmailUpstream string
	// Hostname del servidor de correo (docker-mailserver) — no tiene ruta
	// HTTP propia (Caddy no le hace de proxy, nada le habla por HTTP), pero
	// SÍ necesita estar en la lista de subjects: docker-mailserver reutiliza
	// el certificado que Caddy emite/renueva aquí para servir IMAP/SMTP con
	// TLS (ver SSL_CERT_PATH/SSL_KEY_PATH en docker-compose.yml). Sin esto,
	// Caddy nunca pide el certificado y el mailserver no arranca.
	MailHostname string
	// Página estática que se sirve cuando un sitio está detenido.
	MaintenanceMessage string

	// WAF (Coraza + OWASP CRS) y límite de tasa por IP, aplicados a las
	// rutas de sitios de clientes (no a la del propio panel, para no
	// arriesgar falsos positivos del WAF sobre el API del panel). Solo
	// tiene efecto si el Caddy de borde se compiló con los plugins
	// correspondientes (ver deploy/edge/Dockerfile) — si WAFEnabled es true
	// mal un Caddy sin esos módulos, la carga de configuración fallará.
	WAFEnabled         bool
	CorazaDirectives   string // reglas de Coraza (Include .../SecRuleEngine On)
	RateLimitPerMinute int    // 0 = usa el valor por defecto (240)
}

// Build compone la configuración completa de Caddy a partir de las rutas.
func Build(routes []SiteRoute, opt BuildOptions) (*Config, error) {
	if len(opt.Listen) == 0 {
		opt.Listen = []string{":80", ":443"}
	}
	if opt.MaintenanceMessage == "" {
		opt.MaintenanceMessage = "Este sitio está temporalmente detenido."
	}
	if opt.LogLevel == "" {
		opt.LogLevel = "INFO"
	}

	srv := &Server{
		Listen: opt.Listen,
		Routes: make([]Route, 0, len(routes)+1),
		Logs:   &Logs{DefaultLoggerName: "edge"},
	}

	// El panel primero: así nunca lo tapa un vhost de cliente.
	if opt.PanelHost != "" && opt.PanelUpstream != "" {
		h, err := reverseProxyHandler(opt.PanelUpstream, true)
		if err != nil {
			return nil, err
		}
		srv.Routes = append(srv.Routes, Route{
			Match:    []Match{{Host: []string{opt.PanelHost}}},
			Handle:   []json.RawMessage{h},
			Terminal: true,
		})
	}

	// Webmail, igual tratamiento que el panel: ruta fija que ningún vhost de
	// cliente puede tapar.
	if opt.WebmailHost != "" && opt.WebmailUpstream != "" {
		h, err := reverseProxyHandler(opt.WebmailUpstream, true)
		if err != nil {
			return nil, err
		}
		srv.Routes = append(srv.Routes, Route{
			Match:    []Match{{Host: []string{opt.WebmailHost}}},
			Handle:   []json.RawMessage{h},
			Terminal: true,
		})
	}

	// Orden estable: evita recargas espurias de Caddy cuando nada cambió.
	sorted := make([]SiteRoute, len(routes))
	copy(sorted, routes)
	sort.Slice(sorted, func(i, j int) bool {
		return strings.Join(sorted[i].Hosts, ",") < strings.Join(sorted[j].Hosts, ",")
	})

	// El dominio del propio panel va en la lista de "subjects" con ACME
	// normal, NO en el catch-all on-demand: ese catch-all consulta
	// /tls/authorize, que solo conoce dominios de sitios de clientes —
	// el panel se rechazaría a sí mismo y se quedaría sin certificado.
	subjects := []string{}
	if opt.PanelHost != "" {
		subjects = append(subjects, opt.PanelHost)
	}
	if opt.WebmailHost != "" {
		subjects = append(subjects, opt.WebmailHost)
	}
	if opt.MailHostname != "" {
		subjects = append(subjects, opt.MailHostname)
	}
	for _, r := range sorted {
		if len(r.Hosts) == 0 {
			continue
		}
		subjects = append(subjects, r.Hosts...)

		var handlers []json.RawMessage
		if opt.WAFEnabled {
			// El WAF y el límite de tasa corren antes que cualquier otra
			// cosa: si Coraza bloquea la petición, ni el redirect ni el
			// reverse_proxy llegan a ejecutarse.
			h, err := rateLimitHandler(opt.RateLimitPerMinute)
			if err != nil {
				return nil, err
			}
			handlers = append(handlers, h)
			h, err = wafHandler(opt.CorazaDirectives)
			if err != nil {
				return nil, err
			}
			handlers = append(handlers, h)
		}
		switch {
		case r.RedirectTo != "":
			h, err := redirectHandler(r.RedirectTo)
			if err != nil {
				return nil, err
			}
			handlers = append(handlers, h)
		case r.Offline || r.Upstream == "":
			h, err := staticHandler(503, opt.MaintenanceMessage)
			if err != nil {
				return nil, err
			}
			handlers = append(handlers, h)
		default:
			h, err := reverseProxyHandler(r.Upstream, false)
			if err != nil {
				return nil, err
			}
			handlers = append(handlers, h)
		}

		srv.Routes = append(srv.Routes, Route{
			Match:    []Match{{Host: r.Hosts}},
			Handle:   handlers,
			Terminal: true,
		})
	}

	cfg := &Config{
		Admin: &Admin{Listen: "0.0.0.0:2019"},
		Apps: Apps{
			HTTP: &HTTPApp{Servers: map[string]*Server{"edge": srv}},
		},
	}
	cfg.Logging = &struct {
		Logs map[string]LogConfig `json:"logs,omitempty"`
	}{Logs: map[string]LogConfig{"default": {Level: opt.LogLevel}}}

	// TLS: ACME con el correo configurado; on-demand para dominios de clientes
	// que aún no existen cuando se recarga la configuración.
	if opt.ACMEEmail != "" || opt.OnDemandAskURL != "" {
		policy := TLSPolicy{Subjects: dedupe(subjects)}
		if opt.ACMEEmail != "" {
			issuer, err := json.Marshal(map[string]any{
				"module": "acme",
				"email":  opt.ACMEEmail,
			})
			if err != nil {
				return nil, err
			}
			policy.Issuers = []json.RawMessage{issuer}
		}
		automation := &TLSAutomation{}
		if len(policy.Subjects) > 0 {
			automation.Policies = append(automation.Policies, policy)
		}
		if opt.OnDemandAskURL != "" {
			perm, err := json.Marshal(map[string]any{
				"module":   "http",
				"endpoint": opt.OnDemandAskURL,
			})
			if err != nil {
				return nil, err
			}
			automation.OnDemand = &OnDemand{Permission: perm}
			automation.Policies = append(automation.Policies, TLSPolicy{OnDemand: true})
		}
		tlsApp := &TLSApp{Automation: automation}
		if opt.MailHostname != "" {
			tlsApp.Certificates = &TLSCertificates{Automate: []string{opt.MailHostname}}
		}
		cfg.Apps.TLS = tlsApp
	}

	return cfg, nil
}

func reverseProxyHandler(upstream string, panel bool) (json.RawMessage, error) {
	h := map[string]any{
		"handler":   "reverse_proxy",
		"upstreams": []map[string]string{{"dial": upstream}},
		"headers": map[string]any{
			"request": map[string]any{
				"set": map[string][]string{
					"X-Forwarded-Proto": {"{http.request.scheme}"},
					"X-Forwarded-Host":  {"{http.request.host}"},
					"X-Real-IP":         {"{http.request.remote.host}"},
				},
			},
		},
		"transport": map[string]any{
			"protocol":                "http",
			"dial_timeout":            "10s",
			"response_header_timeout": "120s",
		},
	}
	if panel {
		// El panel necesita streaming para SSE (logs en vivo).
		h["flush_interval"] = -1
	}
	return json.Marshal(h)
}

// wafHandler arma el handler de Coraza (github.com/corazawaf/coraza-caddy),
// el WAF equivalente a mod_security que corre sobre las reglas OWASP CRS ya
// horneadas en la imagen del borde (ver deploy/edge/Dockerfile). El ID de
// módulo JSON es "waf" (http.handlers.waf) — "coraza_waf" es solo el nombre
// de la directiva del Caddyfile, no vale como "handler" en config JSON.
func wafHandler(directives string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"handler":    "waf",
		"directives": directives,
	})
}

// rateLimitHandler arma el handler de github.com/mholt/caddy-ratelimit: un
// límite de peticiones por IP para mitigar abuso/DDoS básico a nivel de
// aplicación (no reemplaza protección de red contra volumetría).
func rateLimitHandler(perMinute int) (json.RawMessage, error) {
	if perMinute <= 0 {
		perMinute = 240
	}
	return json.Marshal(map[string]any{
		"handler": "rate_limit",
		"rate_limits": map[string]any{
			"edge_global": map[string]any{
				"key":        "{http.request.remote.host}",
				"window":     "1m",
				"max_events": perMinute,
			},
		},
	})
}

func redirectHandler(target string) (json.RawMessage, error) {
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "https://" + target
	}
	return json.Marshal(map[string]any{
		"handler":     "static_response",
		"status_code": 301,
		"headers": map[string][]string{
			"Location": {strings.TrimRight(target, "/") + "{http.request.uri}"},
		},
	})
}

func staticHandler(status int, body string) (json.RawMessage, error) {
	return json.Marshal(map[string]any{
		"handler":     "static_response",
		"status_code": status,
		"headers":     map[string][]string{"Content-Type": {"text/html; charset=utf-8"}},
		"body": fmt.Sprintf(
			`<!doctype html><html lang="es"><meta charset="utf-8">`+
				`<title>Sitio no disponible</title>`+
				`<body style="font-family:system-ui;padding:4rem;text-align:center">`+
				`<h1>503</h1><p>%s</p></body></html>`, body),
	})
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}
