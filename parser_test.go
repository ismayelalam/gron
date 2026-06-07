package gron

import (
	"testing"
	"time"
)

func TestCronParser_Matches(t *testing.T) {
	tests := []struct {
		name string
		expr string
		t    time.Time
		want bool
	}{
		{"Every minute", "* * * * *", time.Date(2024, 6, 1, 12, 30, 0, 0, time.UTC), true},
		{"Specific Monday", "30 14 * * 1", time.Date(2024, 6, 3, 14, 30, 0, 0, time.UTC), true},
		{"Wrong minute", "30 14 * * 1", time.Date(2024, 6, 3, 14, 31, 0, 0, time.UTC), false},
		{"Wrong weekday", "0 0 * * 0", time.Date(2024, 6, 3, 0, 0, 0, 0, time.UTC), false},
		{"Hour range", "0 9-17 * * 1-5", time.Date(2024, 6, 4, 12, 0, 0, 0, time.UTC), true},
		{"Step 15 min", "*/15 * * * *", time.Date(2024, 6, 1, 12, 45, 0, 0, time.UTC), true},
		{"List values", "0,30 * * * *", time.Date(2024, 6, 1, 12, 30, 0, 0, time.UTC), true},

		// New tests for the n/m syntax (e.g., 5/2 means starting at 5, every 2)
		{"Start at 5, step 2 (exact)", "5/2 * * * *", time.Date(2024, 6, 1, 12, 5, 0, 0, time.UTC), true},
		{"Start at 5, step 2 (match)", "5/2 * * * *", time.Date(2024, 6, 1, 12, 7, 0, 0, time.UTC), true},
		{"Start at 5, step 2 (no match)", "5/2 * * * *", time.Date(2024, 6, 1, 12, 6, 0, 0, time.UTC), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p, err := NewCronParser(tt.expr)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			if got := p.Matches(tt.t); got != tt.want {
				t.Errorf("Matches(%v) = %v, want %v", tt.t, got, tt.want)
			}
		})
	}
}

func TestCronParser_Invalid(t *testing.T) {
	// Added "@every -5s" to test the negative duration check
	invalid := []string{"60 * * * *", "* 24 * * *", "* * * * * *", "* * *", "@every -5s"}
	for _, expr := range invalid {
		t.Run(expr, func(t *testing.T) {
			if _, err := NewCronParser(expr); err == nil {
				t.Error("expected error for invalid cron expression")
			}
		})
	}
}

func TestCronParser_Next(t *testing.T) {
	p, err := NewCronParser("0 0 * * *")
	if err != nil {
		t.Fatal(err)
	}

	from := time.Date(2024, 6, 1, 12, 0, 0, 0, time.UTC)
	next := p.Next(from)
	want := time.Date(2024, 6, 2, 0, 0, 0, 0, time.UTC)

	if !next.Equal(want) {
		t.Errorf("Next() = %v, want %v", next, want)
	}
}

func TestCronParser_Descriptors(t *testing.T) {
	// Cleaned up: just verify all standard descriptors parse without error
	descriptorsToTest := []string{
		"@yearly",
		"@semiannual",
		"@quarterly",
		"@monthly",
		"@weekly",
		"@daily",
		"@hourly",
	}

	for _, expr := range descriptorsToTest {
		t.Run(expr, func(t *testing.T) {
			if _, err := NewCronParser(expr); err != nil {
				t.Errorf("failed to parse %q: %v", expr, err)
			}
		})
	}
}

func TestCronParser_Every(t *testing.T) {
	p, err := NewCronParser("@every 3s")
	if err != nil {
		t.Fatal(err)
	}
	if !p.every {
		t.Error("expected @every flag to be true")
	}
	if p.everyDuration != 3*time.Second {
		t.Errorf("expected duration 3s, got %v", p.everyDuration)
	}

	// Updated: Next() for @every now simply adds the duration to 'from'
	// (instead of truncating to absolute zero time boundaries)
	from := time.Date(2026, 6, 1, 12, 0, 1, 0, time.UTC)
	next := p.Next(from)
	want := time.Date(2026, 6, 1, 12, 0, 4, 0, time.UTC) // 12:00:01 + 3s
	if !next.Equal(want) {
		t.Errorf("Next() = %v, want %v", next, want)
	}
}

// TestCronParser_EveryForgiving verifies that @every handles extra spaces gracefully
func TestCronParser_EveryForgiving(t *testing.T) {
	tests := []string{
		"@every  5m",
		"@every   1h",
		"@every 10s ",
	}
	for _, expr := range tests {
		t.Run(expr, func(t *testing.T) {
			if _, err := NewCronParser(expr); err != nil {
				t.Errorf("expected forgiving parse for %q, got error: %v", expr, err)
			}
		})
	}
}
