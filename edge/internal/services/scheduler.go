package services

import (
	"log/slog"
	"sync"
	"time"

	"github.com/3to1go/edge/internal/schedule"
)

const (
	startupDelayMinutes = 5
	minimumCycleGap     = schedule.MinimumScheduleMinutes * time.Minute
)

// CycleRunner is anything that can execute one backup cycle.
type CycleRunner interface {
	RunCycle() bool
	Logger() *slog.Logger
	CronSchedule() string
}

// SchedulerController drives timed and on-demand backup cycles.
type SchedulerController struct {
	runner CycleRunner

	mu                sync.Mutex
	sched             *schedule.CronSchedule
	state             string
	nextRunAt         *time.Time
	lastStartedAt     *time.Time
	lastCompletedAt   *time.Time
	runNowRequested   bool
	startupDelayUntil time.Time

	stopCh chan struct{}
	wakeCh chan struct{}
	doneCh chan struct{}
}

func NewSchedulerController(runner CycleRunner) (*SchedulerController, error) {
	sched, err := schedule.Parse(runner.CronSchedule())
	if err != nil {
		return nil, err
	}
	return &SchedulerController{
		runner:            runner,
		sched:             sched,
		state:             "idle",
		startupDelayUntil: time.Now().Add(startupDelayMinutes * time.Minute),
		stopCh:            make(chan struct{}),
		wakeCh:            make(chan struct{}, 1),
		doneCh:            make(chan struct{}),
	}, nil
}

func (s *SchedulerController) Start() {
	go s.loop()
}

func (s *SchedulerController) Stop() {
	close(s.stopCh)
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
	<-s.doneCh
	s.mu.Lock()
	s.state = "stopped"
	s.nextRunAt = nil
	s.mu.Unlock()
}

func (s *SchedulerController) RequestRunNow() string {
	s.mu.Lock()
	if s.state == "running" {
		s.mu.Unlock()
		return "already_running"
	}
	if s.runNowRequested {
		s.mu.Unlock()
		return "queued"
	}
	s.runNowRequested = true
	s.mu.Unlock()

	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
	return "queued"
}

func (s *SchedulerController) ReloadSettings(newSchedule string) error {
	parsed, err := schedule.Parse(newSchedule)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.sched = parsed
	if s.state != "running" {
		s.nextRunAt = nil
	}
	s.mu.Unlock()
	select {
	case s.wakeCh <- struct{}{}:
	default:
	}
	return nil
}

func (s *SchedulerController) Snapshot() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := map[string]any{
		"state":                     s.state,
		"cron_schedule":             s.sched.Expression,
		"minimum_cycle_gap_minutes": schedule.MinimumScheduleMinutes,
		"next_run_at":               formatTime(s.nextRunAt),
		"last_started_at":           formatTime(s.lastStartedAt),
		"last_completed_at":         formatTime(s.lastCompletedAt),
		"run_now_requested":         s.runNowRequested,
	}
	if s.lastCompletedAt == nil {
		t := s.startupDelayUntil
		result["startup_delay_until"] = formatTime(&t)
	}
	return result
}

func (s *SchedulerController) loop() {
	defer close(s.doneCh)
	for {
		select {
		case <-s.stopCh:
			return
		default:
		}

		if s.consumeRunRequest() {
			trigger := "manual"
			s.mu.Lock()
			if s.lastCompletedAt == nil {
				trigger = "startup"
			}
			s.mu.Unlock()
			s.runCycle(trigger)
			continue
		}

		nextRunAt := s.computeNextRunAt()
		s.mu.Lock()
		s.state = "waiting"
		s.nextRunAt = &nextRunAt
		s.mu.Unlock()

		timeout := max(time.Until(nextRunAt), 0)

		select {
		case <-s.stopCh:
			return
		case <-s.wakeCh:
			continue
		case <-time.After(timeout):
			s.runCycle("scheduled")
		}
	}
}

func (s *SchedulerController) consumeRunRequest() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.runNowRequested {
		return false
	}
	s.runNowRequested = false
	s.nextRunAt = nil
	return true
}

func (s *SchedulerController) computeNextRunAt() time.Time {
	s.mu.Lock()
	last := s.lastCompletedAt
	sched := s.sched
	delay := s.startupDelayUntil
	s.mu.Unlock()

	baseline := time.Now()
	if last != nil {
		baseline = *last
	}

	next, err := sched.NextAfter(baseline)
	if err != nil {
		next = time.Now().Add(5 * time.Minute)
	}

	if last == nil {
		if delay.After(next) {
			next = delay
		}
	} else {
		minNext := last.Add(minimumCycleGap)
		if minNext.After(next) {
			next = minNext
		}
	}
	return next
}

func (s *SchedulerController) runCycle(trigger string) {
	now := time.Now()
	s.mu.Lock()
	s.state = "running"
	s.nextRunAt = nil
	s.lastStartedAt = &now
	s.mu.Unlock()

	s.runner.Logger().Info("cycle_started", "trigger", trigger, "schedule", s.sched.Expression)
	defer func() {
		completed := time.Now()
		s.mu.Lock()
		s.state = "idle"
		s.lastCompletedAt = &completed
		s.mu.Unlock()
		s.runner.Logger().Info("cycle_completed", "trigger", trigger)
	}()

	s.runner.RunCycle()
}

func formatTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339)
}
