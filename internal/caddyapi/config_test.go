package caddyapi

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestBuildRoutesPanelFirst(t *testing.T) {
	cfg, err := Build([]SiteRoute{
		{Hosts: []string{"cliente.cl", "www.cliente.cl"}, Upstream: "gocp-site-a:8080"},
	}, BuildOptions{
		PanelHost:     "panel.host.cl",
		PanelUpstream: "gocp-panel:8080",
		ACMEEmail:     "admin@host.cl",
	})
	if err != nil {
		t.Fatal(err)
	}

	srv := cfg.Apps.HTTP.Servers["edge"]
	if len(srv.Routes) != 2 {
		t.Fatalf("se esperaban 2 rutas, hay %d", len(srv.Routes))
	}
	// El panel debe ir primero para que ningún vhost de cliente lo tape.
	if srv.Routes[0].Match[0].Host[0] != "panel.host.cl" {
		t.Errorf("la primera ruta debería ser la del panel, es %v", srv.Routes[0].Match[0].Host)
	}
	if !srv.Routes[0].Terminal {
		t.Error("las rutas deben ser terminales")
	}
	if cfg.Apps.TLS == nil || len(cfg.Apps.TLS.Automation.Policies) == 0 {
		t.Fatal("falta la política de TLS")
	}
}

func TestBuildOfflineSiteServes503(t *testing.T) {
	cfg, err := Build([]SiteRoute{
		{Hosts: []string{"caido.cl"}, Upstream: "", Offline: true},
	}, BuildOptions{MaintenanceMessage: "en mantenimiento"})
	if err != nil {
		t.Fatal(err)
	}

	handler := cfg.Apps.HTTP.Servers["edge"].Routes[0].Handle[0]
	var decoded map[string]any
	if err := json.Unmarshal(handler, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["handler"] != "static_response" {
		t.Errorf("un sitio detenido debe servir static_response, sirve %v", decoded["handler"])
	}
	if decoded["status_code"].(float64) != 503 {
		t.Errorf("se esperaba 503, se obtuvo %v", decoded["status_code"])
	}
	if !strings.Contains(decoded["body"].(string), "en mantenimiento") {
		t.Error("el cuerpo debería incluir el mensaje de mantenimiento")
	}
}

func TestBuildRedirect(t *testing.T) {
	cfg, err := Build([]SiteRoute{
		{Hosts: []string{"viejo.cl"}, RedirectTo: "nuevo.cl"},
	}, BuildOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(cfg.Apps.HTTP.Servers["edge"].Routes[0].Handle[0], &decoded); err != nil {
		t.Fatal(err)
	}
	headers := decoded["headers"].(map[string]any)
	loc := headers["Location"].([]any)[0].(string)
	if !strings.HasPrefix(loc, "https://nuevo.cl") {
		t.Errorf("la redirección debería apuntar a https://nuevo.cl, apunta a %s", loc)
	}
	if decoded["status_code"].(float64) != 301 {
		t.Errorf("se esperaba un 301, se obtuvo %v", decoded["status_code"])
	}
}

func TestBuildIsDeterministic(t *testing.T) {
	routes := []SiteRoute{
		{Hosts: []string{"zeta.cl"}, Upstream: "z:8080"},
		{Hosts: []string{"alfa.cl"}, Upstream: "a:8080"},
	}
	first, _ := Build(routes, BuildOptions{})
	second, _ := Build([]SiteRoute{routes[1], routes[0]}, BuildOptions{})

	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Error("la configuración debe ser estable ante el orden de entrada")
	}
}

func TestDedupe(t *testing.T) {
	got := dedupe([]string{"b.cl", "a.cl", "b.cl", ""})
	if len(got) != 2 || got[0] != "a.cl" || got[1] != "b.cl" {
		t.Errorf("dedupe devolvió %v", got)
	}
}
