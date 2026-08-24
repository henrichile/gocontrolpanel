package hostctl

import (
	"regexp"
	"strconv"
	"strings"
)

type Rule struct {
	Port    int    `json:"port"`
	Proto   string `json:"proto"`
	Action  string `json:"action"` // "allow" | "deny"
	From    string `json:"from"`
	Comment string `json:"comment,omitempty"`
}

// Status es el resultado de normalizar la salida de "status" del script del
// host (ver install.sh).
type Status struct {
	// Enabled solo es significativo si EnabledKnown es true — un script del
	// host instalado antes de que "status" reportara el estado global (hace
	// falta volver a correr install.sh para tenerlo) simplemente no manda la
	// línea "GLOBAL", y no hay forma de saber el estado sin adivinar.
	Enabled      bool
	EnabledKnown bool
	Rules        []Rule
}

// rePortProto matchea el primer campo de cada línea de regla: "22/tcp".
var rePortProto = regexp.MustCompile(`^(\d{1,5})/(tcp|udp)$`)

// ParseStatus interpreta la salida ya normalizada por el script del host:
// una primera línea opcional "GLOBAL<TAB>enabled|disabled" y, después, una
// regla por línea en el formato nuevo —
// "PUERTO/PROTO<TAB>allow|deny<TAB>ORIGEN<TAB>COMENTARIO" (TAB como
// separador para que el comentario pueda traer espacios) — o, por
// compatibilidad con un script que todavía no se actualizó, el formato
// viejo "PUERTO/PROTO allow|deny" (sin origen ni comentario).
func ParseStatus(raw string) Status {
	var st Status
	seen := map[string]bool{}
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		if rest, ok := strings.CutPrefix(line, "GLOBAL\t"); ok {
			st.Enabled = strings.TrimSpace(rest) == "enabled"
			st.EnabledKnown = true
			continue
		}
		r, ok := parseRuleLine(line)
		if !ok {
			continue
		}
		// ufw reporta v4/v6 como filas separadas para la misma regla; se
		// deduplica acá por la tupla completa (no solo puerto/proto: dos
		// reglas legítimas pueden compartir puerto con distinto origen).
		key := strconv.Itoa(r.Port) + "/" + r.Proto + "/" + r.Action + "/" + r.From
		if seen[key] {
			continue
		}
		seen[key] = true
		st.Rules = append(st.Rules, r)
	}
	return st
}

func parseRuleLine(line string) (Rule, bool) {
	var portProto, action, origin, comment string
	if strings.Contains(line, "\t") {
		parts := strings.SplitN(line, "\t", 4)
		portProto = strings.TrimSpace(parts[0])
		if len(parts) > 1 {
			action = strings.TrimSpace(parts[1])
		}
		if len(parts) > 2 {
			origin = strings.TrimSpace(parts[2])
		}
		if len(parts) > 3 {
			comment = parts[3]
		}
	} else {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return Rule{}, false
		}
		portProto, action = fields[0], fields[1]
	}

	m := rePortProto.FindStringSubmatch(portProto)
	if m == nil {
		return Rule{}, false
	}
	if action != "allow" && action != "deny" {
		return Rule{}, false
	}
	port, err := strconv.Atoi(m[1])
	if err != nil {
		return Rule{}, false
	}
	if origin == "" {
		origin = "Anywhere"
	}
	return Rule{Port: port, Proto: m[2], Action: action, From: origin, Comment: comment}, true
}
