package schedule

import (
	"fmt"
	"strings"
	"time"

	"github.com/3to1go/edge/internal/config"
	"github.com/robfig/cron/v3"
)

const MinimumScheduleMinutes = 5

var (
	minimumScheduleGap = time.Duration(MinimumScheduleMinutes) * time.Minute
	cronParser         = cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
)

// CronSchedule holds a parsed 5-field cron expression.
type CronSchedule struct {
	Expression string
	schedule   cron.Schedule
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
	parts := strings.Fields(normalized)
	if len(parts) != 5 {
		return nil, fmt.Errorf("cron schedule must use 5 fields like '0 2 * * 0'")
	}

	parsed, err := cronParser.Parse(normalized)
	if err != nil {
		return nil, fmt.Errorf("invalid cron schedule: %w", err)
	}

	s := &CronSchedule{
		Expression: normalized,
		schedule:   parsed,
	}
	if err := s.validateMinimumSpacing(); err != nil {
		return nil, err
	}
	return s, nil
}

// NextAfter returns the next scheduled time strictly after after.
func (s *CronSchedule) NextAfter(after time.Time) (time.Time, error) {
	if s == nil || s.schedule == nil {
		return time.Time{}, fmt.Errorf("cron schedule is not initialized")
	}
	return s.schedule.Next(after), nil
}

func (s *CronSchedule) validateMinimumSpacing() error {
	reference := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).Add(-time.Minute)
	previous, err := s.NextAfter(reference)
	if err != nil {
		return err
	}
	for range 128 {
		current, err := s.NextAfter(previous)
		if err != nil {
			return err
		}
		if current.IsZero() {
			return fmt.Errorf("cron schedule has no future occurrences")
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
