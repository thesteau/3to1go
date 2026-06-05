package schedule

import (
	"testing"
	"time"
)

// --- Parse: valid expressions ---

func TestParse_ValidExpressions(t *testing.T) {
	cases := []string{
		"0 2 * * 0",    // Sundays at 2am
		"0 */6 * * *",  // every 6 hours
		"0 0 1 * *",    // first of month at midnight
		"30 12 * * 1-5", // weekdays at noon
		"0 8,12,18 * * *", // three times a day
	}
	for _, expr := range cases {
		if _, err := Parse(expr); err != nil {
			t.Errorf("Parse(%q) unexpected error: %v", expr, err)
		}
	}
}

// --- Parse: field count errors ---

func TestParse_TooFewFields(t *testing.T) {
	if _, err := Parse("0 2 * *"); err == nil {
		t.Error("expected error for 4-field expression")
	}
}

func TestParse_TooManyFields(t *testing.T) {
	if _, err := Parse("0 2 * * 0 extra"); err == nil {
		t.Error("expected error for 6-field expression")
	}
}

// --- Parse: field range validation ---

func TestParse_MinuteOutOfRange(t *testing.T) {
	if _, err := Parse("60 2 * * 0"); err == nil {
		t.Error("expected error for minute=60")
	}
}

func TestParse_HourOutOfRange(t *testing.T) {
	if _, err := Parse("0 24 * * 0"); err == nil {
		t.Error("expected error for hour=24")
	}
}

func TestParse_DayOfMonthZero(t *testing.T) {
	if _, err := Parse("0 2 0 * 0"); err == nil {
		t.Error("expected error for day_of_month=0")
	}
}

func TestParse_MonthOutOfRange(t *testing.T) {
	if _, err := Parse("0 2 * 13 0"); err == nil {
		t.Error("expected error for month=13")
	}
}

func TestParse_InvalidRangeStartGtEnd(t *testing.T) {
	if _, err := Parse("0 18-8 * * *"); err == nil {
		t.Error("expected error for inverted range 18-8")
	}
}

func TestParse_InvalidStepZero(t *testing.T) {
	if _, err := Parse("*/0 * * * *"); err == nil {
		t.Error("expected error for step=0")
	}
}

// --- Parse: minimum spacing enforcement ---

func TestParse_EveryMinuteTooFrequent(t *testing.T) {
	if _, err := Parse("* * * * *"); err == nil {
		t.Error("expected error for every-minute schedule (too frequent)")
	}
}

func TestParse_Every4MinutesTooFrequent(t *testing.T) {
	if _, err := Parse("*/4 * * * *"); err == nil {
		t.Error("expected error for every-4-minute schedule (too frequent)")
	}
}

func TestParse_Every5MinutesAllowed(t *testing.T) {
	if _, err := Parse("*/5 * * * *"); err != nil {
		t.Errorf("expected no error for every-5-minute schedule, got: %v", err)
	}
}

// --- Parse: field syntax ---

func TestParse_DayOfWeek7NormalizedToSunday(t *testing.T) {
	s, err := Parse("0 2 * * 7")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.daysOfWeek[0] {
		t.Error("expected day_of_week 7 to be normalized to 0 (Sunday)")
	}
}

func TestParse_StepSyntaxHours(t *testing.T) {
	s, err := Parse("0 */6 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range []int{0, 6, 12, 18} {
		if !s.hours[h] {
			t.Errorf("expected hour %d to be set", h)
		}
	}
	for _, h := range []int{1, 5, 7, 11, 13, 17, 19} {
		if s.hours[h] {
			t.Errorf("expected hour %d to be unset", h)
		}
	}
}

func TestParse_RangeSyntax(t *testing.T) {
	s, err := Parse("0 9-17 * * 1-5")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range []int{9, 10, 11, 12, 13, 14, 15, 16, 17} {
		if !s.hours[h] {
			t.Errorf("expected hour %d to be set", h)
		}
	}
	if s.hours[8] || s.hours[18] {
		t.Error("hours outside range should not be set")
	}
	for _, d := range []int{1, 2, 3, 4, 5} {
		if !s.daysOfWeek[d] {
			t.Errorf("expected dow %d to be set", d)
		}
	}
	if s.daysOfWeek[0] || s.daysOfWeek[6] {
		t.Error("weekend days should not be set")
	}
}

func TestParse_CommaSyntax(t *testing.T) {
	s, err := Parse("0 8,12,18 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, h := range []int{8, 12, 18} {
		if !s.hours[h] {
			t.Errorf("expected hour %d to be set", h)
		}
	}
	if s.hours[9] || s.hours[11] || s.hours[19] {
		t.Error("unlisted hours should not be set")
	}
}

func TestParse_WildcardFields(t *testing.T) {
	// "0 2 * * *" — both day-of-month and day-of-week are wildcards.
	s, err := Parse("0 2 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s.dayOfMonthIsWildcard {
		t.Error("expected dayOfMonthIsWildcard=true for *")
	}
	if !s.dayOfWeekIsWildcard {
		t.Error("expected dayOfWeekIsWildcard=true for *")
	}

	// "0 2 * * 0" — day-of-month is wildcard, day-of-week is specific (Sunday=0).
	s2, err := Parse("0 2 * * 0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !s2.dayOfMonthIsWildcard {
		t.Error("expected dayOfMonthIsWildcard=true when dom is *")
	}
	if s2.dayOfWeekIsWildcard {
		t.Error("expected dayOfWeekIsWildcard=false when dow is specific (0=Sunday)")
	}
}

// --- NextAfter ---

func TestNextAfter_WeeklySunday(t *testing.T) {
	s, err := Parse("0 2 * * 0")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// Start from a Monday Jan 5 2026
	ref := time.Date(2026, 1, 5, 0, 0, 0, 0, time.UTC)
	next, err := s.NextAfter(ref)
	if err != nil {
		t.Fatalf("NextAfter: %v", err)
	}
	// Next Sunday Jan 11 at 2am
	want := time.Date(2026, 1, 11, 2, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v", next, want)
	}
}

func TestNextAfter_Every6Hours(t *testing.T) {
	s, err := Parse("0 */6 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ref := time.Date(2026, 1, 1, 0, 30, 0, 0, time.UTC)
	next, err := s.NextAfter(ref)
	if err != nil {
		t.Fatalf("NextAfter: %v", err)
	}
	want := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v", next, want)
	}
}

func TestNextAfter_MidnightRollover(t *testing.T) {
	s, err := Parse("0 0 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ref := time.Date(2026, 1, 1, 23, 30, 0, 0, time.UTC)
	next, err := s.NextAfter(ref)
	if err != nil {
		t.Fatalf("NextAfter: %v", err)
	}
	want := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v", next, want)
	}
}

func TestNextAfter_MonthBoundary(t *testing.T) {
	// First of each month at noon
	s, err := Parse("0 12 1 * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	ref := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	next, err := s.NextAfter(ref)
	if err != nil {
		t.Fatalf("NextAfter: %v", err)
	}
	want := time.Date(2026, 2, 1, 12, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v", next, want)
	}
}

func TestNextAfter_StrictlyAfterExactMatch(t *testing.T) {
	s, err := Parse("0 */6 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	// ref is exactly on a scheduled time — NextAfter must return the one after
	ref := time.Date(2026, 1, 1, 6, 0, 0, 0, time.UTC)
	next, err := s.NextAfter(ref)
	if err != nil {
		t.Fatalf("NextAfter: %v", err)
	}
	want := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Errorf("got %v, want %v (must be strictly after ref)", next, want)
	}
}

// --- matches helpers ---

func TestMatches_BothWild(t *testing.T) {
	s, err := Parse("0 12 * * *")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if !s.matches(time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)) {
		t.Error("expected match at noon")
	}
	if s.matches(time.Date(2026, 1, 1, 13, 0, 0, 0, time.UTC)) {
		t.Error("expected no match at 1pm")
	}
}

func TestMatches_WrongMinute(t *testing.T) {
	s, err := Parse("0 2 * * 0")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if s.matches(time.Date(2026, 1, 11, 2, 1, 0, 0, time.UTC)) {
		t.Error("expected no match at 2:01")
	}
}
