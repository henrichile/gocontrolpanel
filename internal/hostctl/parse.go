package hostctl

import (
	"regexp"
	"strconv"
	"strings"
)

type Rule struct {
	Port   int    `json:"port"`
	Proto  string `json:"proto"`
	Action string `json:"action"` // "allow" | "deny"
	From   string `json:"from"`   // "Anywhere" salvo que la regla restrinja origen
}

// reRuleLine matchea líneas de "ufw status verbose" con forma
// "22/tcp                     ALLOW IN    Anywhere" (columnas separadas por
// espacios, ancho variable).
var reRuleLine = regexp.MustCompile(`^(\d{1,5})(?:/(tcp|udp))?\s+(ALLOW|DENY|LIMIT|REJECT)\s+IN\s+(.+)$`)

// ParseStatus interpreta la salida de "ufw status verbose". Solo se quedan
// con las reglas de puerto simple (lo que la UI puede administrar); reglas
// más elaboradas (rangos, por interfaz, etc.) se ignoran acá pero siguen
// aplicando en el firewall real — esto es una vista simplificada, no el
// espejo completo de ufw.
func ParseStatus(raw string) []Rule {
	var out []Rule
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		m := reRuleLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		port, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		proto := m[2]
		if proto == "" {
			proto = "tcp" // ufw omite "/tcp" cuando no hay ambigüedad con udp
		}
		action := "allow"
		if m[3] != "ALLOW" {
			action = "deny"
		}
		out = append(out, Rule{Port: port, Proto: proto, Action: action, From: strings.TrimSpace(m[4])})
	}
	return out
}
