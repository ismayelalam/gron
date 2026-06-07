package gron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// cronField represents a parsed cron field with valid values
type cronField struct {
	values map[int]struct{} // set of valid values
}

// matches checks if a value is in the allowed set
func (cf *cronField) matches(v int) bool {
	_, ok := cf.values[v]
	return ok
}

// CronParser parses and evaluates standard 5-field cron expressions.
//
// Supports: *, */n, n, n-m, n,m operators. Timezone-aware matching
// via time.Location. Use NewCronParser to create an instance.
type CronParser struct {
	expression    string
	fields        [5]cronField
	every         bool
	everyDuration time.Duration
}

// Standard cron descriptors mapped to 5-field equivalents
var descriptors = map[string]string{
	"@yearly":     "0 0 1 1 *",
	"@semiannual": "0 0 1 */6 *",
	"@quarterly":  "0 0 1 */3 *",
	"@monthly":    "0 0 1 * *",
	"@weekly":     "0 0 * * 0",
	"@daily":      "0 0 * * *",
	"@hourly":     "0 * * * *",
}

// NewCronParser creates a new parser for the given cron expression.
//
// Expression format: "minute hour day-of-month month day-of-week"
// Example: "0 0 * * *" = midnight daily.
// Returns an error if the expression is syntactically invalid.
func NewCronParser(expr string) (*CronParser, error) {
	expr = strings.TrimSpace(expr)
	p := &CronParser{expression: expr}

	// Handle @every <duration>
	if strings.HasPrefix(expr, "@every") {
		durStr := strings.TrimSpace(strings.TrimPrefix(expr, "@every"))
		dur, err := time.ParseDuration(durStr)
		if err != nil {
			return nil, fmt.Errorf("invalid duration in %q: %w", expr, err)
		}
		if dur <= 0 {
			return nil, fmt.Errorf("duration in %q must be positive", expr)
		}
		p.every = true
		p.everyDuration = dur
		return p, nil
	}

	// Handle standard descriptors
	if std, ok := descriptors[expr]; ok {
		expr = std
	}

	parts := strings.Fields(expr)
	if len(parts) != 5 {
		return nil, fmt.Errorf("expected 5 cron fields, got %d", len(parts))
	}

	constraints := [][2]int{
		{0, 59}, // minute
		{0, 23}, // hour
		{1, 31}, // day of month
		{1, 12}, // month
		{0, 6},  // day of week
	}

	for i, part := range parts {
		field, err := parseField(part, constraints[i][0], constraints[i][1])
		if err != nil {
			return nil, fmt.Errorf("field %d (%q): %w", i+1, part, err)
		}
		p.fields[i] = field
	}
	return p, nil
}

// parseField parses a single cron field component
func parseField(field string, min, max int) (cronField, error) {
	values := make(map[int]struct{})
	for part := range strings.SplitSeq(field, ",") {
		if err := parsePart(part, min, max, values); err != nil {
			return cronField{}, err
		}
	}
	if len(values) == 0 {
		return cronField{}, fmt.Errorf("no valid values parsed")
	}
	return cronField{values: values}, nil
}

// parsePart handles *, */n, n, n-m, n/m formats
func parsePart(part string, min, max int, values map[int]struct{}) error {
	step := 1
	if strings.Contains(part, "/") {
		parts := strings.Split(part, "/")
		if len(parts) != 2 {
			return fmt.Errorf("invalid step format")
		}
		s, err := strconv.Atoi(parts[1])
		if err != nil || s <= 0 {
			return fmt.Errorf("invalid step value")
		}
		step = s
		part = parts[0]
	}

	if part == "*" {
		for i := min; i <= max; i += step {
			values[i] = struct{}{}
		}
		return nil
	}

	if strings.Contains(part, "-") {
		parts := strings.Split(part, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid range format")
		}
		start, err := strconv.Atoi(parts[0])
		if err != nil {
			return fmt.Errorf("invalid range start")
		}
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid range end")
		}
		if start < min || end > max || start > end {
			return fmt.Errorf("range out of bounds")
		}
		for i := start; i <= end; i += step {
			values[i] = struct{}{}
		}
		return nil
	}

	val, err := strconv.Atoi(part)
	if err != nil {
		return fmt.Errorf("invalid value")
	}
	if val < min || val > max {
		return fmt.Errorf("value out of bounds [%d-%d]", min, max)
	}
	if step > 1 {
		for i := val; i <= max; i += step {
			values[i] = struct{}{}
		}
	} else {
		values[val] = struct{}{}
	}
	return nil
}

// Matches reports whether the given time satisfies the cron expression.
//
// Timezone-aware: use t.In(location) if evaluating in a specific timezone.
// Returns true if all five fields match the time's components.
func (p *CronParser) Matches(t time.Time) bool {
	if p.every {
		return true // Interval-based schedules are handled via Next()
	}
	if !p.fields[0].matches(t.Minute()) {
		return false
	}
	if !p.fields[1].matches(t.Hour()) {
		return false
	}
	if !p.fields[2].matches(t.Day()) {
		return false
	}
	if !p.fields[3].matches(int(t.Month())) {
		return false
	}
	if !p.fields[4].matches(int(t.Weekday())) {
		return false
	}
	return true
}

// Next returns the next time after 'from' that matches the cron expression.
//
// Searches forward minute-by-minute. For complex expressions or long gaps,
// consider optimizing with a smarter algorithm. Returns a far-future time
// if no match is found within 1 year.
func (p *CronParser) Next(from time.Time) time.Time {
	if p.every {
		return from.Add(p.everyDuration)
	}
	t := from.Truncate(time.Minute).Add(time.Minute)
	maxIterations := 366 * 24 * 60 // 1 year safety limit
	for range maxIterations {
		if p.Matches(t) {
			return t
		}
		t = t.Add(time.Minute)
	}
	return from.AddDate(100, 0, 0)
}
