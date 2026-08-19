package worker

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Matches evalúa una expresión cron de 5 campos (minuto hora día mes día-semana)
// contra un instante concreto. Soporta *, listas (1,2), rangos (1-5) y pasos
// (*/5, 1-30/2), que es lo que usa el 99 % de los cron de hosting.
func Matches(expr string, t time.Time) (bool, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return false, fmt.Errorf("se esperaban 5 campos, hay %d", len(fields))
	}

	checks := []struct {
		field    string
		value    int
		min, max int
	}{
		{fields[0], t.Minute(), 0, 59},
		{fields[1], t.Hour(), 0, 23},
		{fields[2], t.Day(), 1, 31},
		{fields[3], int(t.Month()), 1, 12},
		{fields[4], int(t.Weekday()), 0, 6},
	}

	for i, c := range checks {
		ok, err := fieldMatches(c.field, c.value, c.min, c.max)
		if err != nil {
			return false, fmt.Errorf("campo %d (%q): %w", i+1, c.field, err)
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func fieldMatches(field string, value, min, max int) (bool, error) {
	for _, part := range strings.Split(field, ",") {
		ok, err := partMatches(strings.TrimSpace(part), value, min, max)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return false, nil
}

func partMatches(part string, value, min, max int) (bool, error) {
	if part == "" {
		return false, fmt.Errorf("elemento vacío")
	}

	step := 1
	if base, stepStr, hasStep := strings.Cut(part, "/"); hasStep {
		n, err := strconv.Atoi(stepStr)
		if err != nil || n <= 0 {
			return false, fmt.Errorf("paso inválido %q", stepStr)
		}
		step = n
		part = base
	}

	lo, hi := min, max
	switch {
	case part == "*":
		// rango completo
	case strings.Contains(part, "-"):
		a, b, _ := strings.Cut(part, "-")
		var err error
		if lo, err = strconv.Atoi(a); err != nil {
			return false, fmt.Errorf("rango inválido %q", part)
		}
		if hi, err = strconv.Atoi(b); err != nil {
			return false, fmt.Errorf("rango inválido %q", part)
		}
	default:
		n, err := strconv.Atoi(part)
		if err != nil {
			return false, fmt.Errorf("valor inválido %q", part)
		}
		// El domingo se acepta tanto como 0 como 7.
		if max == 6 && n == 7 {
			n = 0
		}
		lo, hi = n, n
	}

	if lo < min || hi > max || lo > hi {
		return false, fmt.Errorf("fuera del rango permitido (%d-%d)", min, max)
	}
	if value < lo || value > hi {
		return false, nil
	}
	return (value-lo)%step == 0, nil
}
