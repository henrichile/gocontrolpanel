package worker

import (
	"testing"
	"time"
)

func at(s string) time.Time {
	t, err := time.Parse("2006-01-02 15:04", s)
	if err != nil {
		panic(err)
	}
	return t
}

func TestMatches(t *testing.T) {
	cases := []struct {
		expr string
		when string
		want bool
	}{
		{"* * * * *", "2026-08-17 10:30", true},
		{"30 10 * * *", "2026-08-17 10:30", true},
		{"30 10 * * *", "2026-08-17 10:31", false},
		{"*/5 * * * *", "2026-08-17 10:30", true},
		{"*/5 * * * *", "2026-08-17 10:32", false},
		{"0 */6 * * *", "2026-08-17 12:00", true},
		{"0 */6 * * *", "2026-08-17 13:00", false},
		{"0 0 1 * *", "2026-08-01 00:00", true},
		{"0 0 1 * *", "2026-08-02 00:00", false},
		// 2026-08-17 es lunes (weekday 1).
		{"0 9 * * 1", "2026-08-17 09:00", true},
		{"0 9 * * 2", "2026-08-17 09:00", false},
		{"0 9 * * 1-5", "2026-08-17 09:00", true},
		{"0 9 * * 6,0", "2026-08-17 09:00", false},
		{"15,45 * * * *", "2026-08-17 10:45", true},
		{"15,45 * * * *", "2026-08-17 10:46", false},
		{"10-20/5 * * * *", "2026-08-17 10:15", true},
		{"10-20/5 * * * *", "2026-08-17 10:16", false},
		// El domingo se acepta como 0 y como 7. 2026-08-16 es domingo.
		{"0 0 * * 7", "2026-08-16 00:00", true},
	}

	for _, c := range cases {
		got, err := Matches(c.expr, at(c.when))
		if err != nil {
			t.Errorf("Matches(%q, %q) devolvió error: %v", c.expr, c.when, err)
			continue
		}
		if got != c.want {
			t.Errorf("Matches(%q, %q) = %v, se esperaba %v", c.expr, c.when, got, c.want)
		}
	}
}

func TestMatchesInvalid(t *testing.T) {
	invalid := []string{
		"",
		"* * * *",
		"* * * * * *",
		"60 * * * *",
		"* 25 * * *",
		"abc * * * *",
		"*/0 * * * *",
	}
	for _, expr := range invalid {
		if _, err := Matches(expr, time.Now()); err == nil {
			t.Errorf("Matches(%q) debería haber fallado", expr)
		}
	}
}
