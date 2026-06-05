package schedule

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/3to1go/edge/internal/config"
)

const MinimumScheduleMinutes = 5

var minimumScheduleGap = time.Duration(MinimumScheduleMinutes) * time.Minute
var maxSearchMinutes = 5 * 366 * 24 * 60

type fieldSpec struct {
	name       string
	min, max   int
	isDayOfWek bool
}

var fieldSpecs = []fieldSpec{
	{"minute", 0, 59, false},
	{"hour", 0, 23, false},
	{"day_of_month", 1, 31, false},
	{"month", 1, 12, false},
	{"day_of_week", 0, 6, true},
}

// CronSchedule holds a parsed 5-field cron expression.
type CronSchedule struct {
	Expression           string
	minutes              map[int]bool
	hours                map[int]bool
	daysOfMonth          map[int]bool
	months               map[int]bool
	daysOfWeek           map[int]bool
	dayOfMonthIsWildcard bool
	dayOfWeekIsWildcard  bool
}

func init() {
	config.RegisterCronValidator(func(expr string) error {
		_, err := Parse(expr)
		return err
	})
}

// Parse validates and parses a 5-field cron expression.
func Parse(expression string) (*CronSchedule, error) {
	normalized := strings.Join(strings.Fields(expression), " ")
	parts := strings.Split(normalized, " ")
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron schedule must use 5 fields like '0 2 * * 0'")
	}

	s := &CronSchedule{Expression: normalized}
	wildcards := make([]bool, 5)
	sets := make([]map[int]bool, 5)

	for i, spec := range fieldSpecs {
		vals, isWild, err := parseField(parts[i], spec.min, spec.max, spec.isDayOfWek)
		if err != nil {
			return nil, fmt.Errorf("field %s: %w", spec.name, err)
		}
		sets[i] = vals
		wildcards[i] = isWild
	}

	s.minutes = sets[0]
	s.hours = sets[1]
	s.daysOfMonth = sets[2]
	s.months = sets[3]
	s.daysOfWeek = sets[4]
	s.dayOfMonthIsWildcard = wildcards[2]
	s.dayOfWeekIsWildcard = wildcards[4]

	if err := s.validateMinimumSpacing(); err != nil {
		return nil, err
	}
	return s, nil
}

// NextAfter returns the next scheduled time strictly after `after`.
func (s *CronSchedule) NextAfter(after time.Time) (time.Time, error) {
	candidate := after.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < maxSearchMinutes; i++ {
		if s.matches(candidate) {
			return candidate, nil
		}
		candidate = candidate.Add(time.Minute)
	}
	return time.Time{}, fmt.Errorf("unable to find next cron occurrence for '%s' within one year", s.Expression)
}

func (s *CronSchedule) matches(t time.Time) bool {
	if !s.minutes[t.Minute()] || !s.hours[t.Hour()] || !s.months[int(t.Month())] {
		return false
	}
	domMatch := s.daysOfMonth[t.Day()]
	dowMatch := s.daysOfWeek[cronDayOfWeek(t)]

	if s.dayOfMonthIsWildcard && s.dayOfWeekIsWildcard {
		return true
	}
	if s.dayOfMonthIsWildcard {
		return dowMatch
	}
	if s.dayOfWeekIsWildcard {
		return domMatch
	}
	return domMatch || dowMatch
}

func (s *CronSchedule) validateMinimumSpacing() error {
	reference := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Minute)
	previous, err := s.NextAfter(reference)
	if err != nil {
		return err
	}
	for i := 0; i < 128; i++ {
		current, err := s.NextAfter(previous)
		if err != nil {
			return err
		}
		if current.Sub(previous) < minimumScheduleGap {
			return fmt.Errorf("cron schedule must not run more often than every %d minutes", MinimumScheduleMinutes)
		}
		if current.Sub(previous) > 7*24*time.Hour {
			return nil
		}
		previous = current
	}
	return nil
}

func parseField(field string, min, max int, isDayOfWeek bool) (map[int]bool, bool, error) {
	if field == "*" {
		m := make(map[int]bool)
		for i := min; i <= max; i++ {
			m[i] = true
		}
		return m, true, nil
	}
	values := make(map[int]bool)
	for _, chunk := range strings.Split(field, ",") {
		expanded, err := expandChunk(strings.TrimSpace(chunk), min, max, isDayOfWeek)
		if err != nil {
			return nil, false, err
		}
		for v := range expanded {
			values[v] = true
		}
	}
	if len(values) == 0 {
		return nil, false, fmt.Errorf("invalid cron field '%s'", field)
	}
	return values, false, nil
}

func expandChunk(chunk string, min, max int, isDayOfWeek bool) (map[int]bool, error) {
	if chunk == "" {
		return nil, fmt.Errorf("cron field contains an empty segment")
	}
	base, step, err := splitStep(chunk)
	if err != nil {
		return nil, err
	}
	if step <= 0 {
		return nil, fmt.Errorf("cron step values must be positive")
	}
	start, end, err := parseBaseRange(base, min, max, isDayOfWeek)
	if err != nil {
		return nil, err
	}
	values := make(map[int]bool)
	for v := start; v <= end; v += step {
		nv, err := normalizeValue(v, min, max, isDayOfWeek)
		if err != nil {
			return nil, err
		}
		values[nv] = true
	}
	return values, nil
}

func splitStep(chunk string) (string, int, error) {
	if !strings.Contains(chunk, "/") {
		return chunk, 1, nil
	}
	parts := strings.SplitN(chunk, "/", 2)
	step, err := strconv.Atoi(parts[1])
	if err != nil {
		return "", 0, fmt.Errorf("invalid cron step '%s'", parts[1])
	}
	return parts[0], step, nil
}

func parseBaseRange(base string, min, max int, isDayOfWeek bool) (int, int, error) {
	if base == "*" {
		return min, max, nil
	}
	if strings.Contains(base, "-") {
		parts := strings.SplitN(base, "-", 2)
		start, err := parseValue(parts[0], min, max, isDayOfWeek)
		if err != nil {
			return 0, 0, err
		}
		end, err := parseValue(parts[1], min, max, isDayOfWeek)
		if err != nil {
			return 0, 0, err
		}
		if start > end {
			return 0, 0, fmt.Errorf("invalid cron range '%s'", base)
		}
		return start, end, nil
	}
	v, err := parseValue(base, min, max, isDayOfWeek)
	if err != nil {
		return 0, 0, err
	}
	return v, v, nil
}

func parseValue(s string, min, max int, isDayOfWeek bool) (int, error) {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("invalid cron value '%s'", s)
	}
	return normalizeValue(v, min, max, isDayOfWeek)
}

func normalizeValue(v, min, max int, isDayOfWeek bool) (int, error) {
	if isDayOfWeek && v == 7 {
		v = 0
	}
	if v < min || v > max {
		return 0, fmt.Errorf("cron value '%d' must be between %d and %d", v, min, max)
	}
	return v, nil
}

// cronDayOfWeek returns 0=Sunday … 6=Saturday (matches cron and Go's time.Weekday).
func cronDayOfWeek(t time.Time) int {
	return int(t.Weekday())
}
