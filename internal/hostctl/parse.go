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
	From   string `json:"from"`
}

// reNormalizedLine matchea el formato normalizado que el propio script del
// host emite para "status" (ver install.sh) — "22/tcp allow" — igual para
// ufw y firewalld, para no tener que parsear el formato humano de cada
// herramienta (frágil y distinto entre las dos).
var reNormalizedLine = regexp.MustCompile(`^(\d{1,5})/(tcp|udp)\s+(allow|deny)$`)

// ParseStatus interpreta la salida ya normalizada por el script del host.
func ParseStatus(raw string) []Rule {
	seen := map[string]bool{}
	var out []Rule
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimSpace(line)
		m := reNormalizedLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if seen[line] {
			continue // ufw reporta v4/v6 como filas separadas; se deduplica acá
		}
		seen[line] = true
		port, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		out = append(out, Rule{Port: port, Proto: m[2], Action: m[3], From: "Anywhere"})
	}
	return out
}
