package services

import (
	"io"
	"log/slog"
	"testing"
	"time"
)

type fakeCycleRunner struct {
	schedule string
	runs     int
}

func (r *fakeCycleRunner) RunCycle() bool {
	r.runs++
	return true
}

func (r *fakeCycleRunner) Logger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func (r *fakeCycleRunner) CronSchedule() string {
	return r.schedule
}

func TestSchedulerControllerLifecycleAndSnapshot(t *testing.T) {
	runner := &fakeCycleRunner{schedule: "*/30 * * * *"}
	controller, err := NewSchedulerController(runner)
	if err != nil {
		t.Fatalf("NewSchedulerController: %v", err)
	}
	snap := controller.Snapshot()
	if snap["state"] != "idle" || snap["cron_schedule"] != runner.schedule || snap["startup_delay_until"] == nil {
		t.Fatalf("initial snapshot = %+v", snap)
	}
	if got := controller.RequestRunNow(); got != "queued" {
		t.Fatalf("RequestRunNow = %q", got)
	}
	if got := controller.RequestRunNow(); got != "queued" {
		t.Fatalf("RequestRunNow duplicate = %q", got)
	}
	if !controller.consumeRunRequest() {
		t.Fatal("expected queued run request")
	}
	if controller.consumeRunRequest() {
		t.Fatal("second consume should be false")
	}

	controller.runCycle("manual")
	if runner.runs != 1 {
		t.Fatalf("runs = %d", runner.runs)
	}
	snap = controller.Snapshot()
	if snap["state"] != "idle" || snap["last_started_at"] == nil || snap["last_completed_at"] == nil {
		t.Fatalf("post-run snapshot = %+v", snap)
	}
}

func TestSchedulerReloadAndTimeHelpers(t *testing.T) {
	runner := &fakeCycleRunner{schedule: "*/30 * * * *"}
	controller, err := NewSchedulerController(runner)
	if err != nil {
		t.Fatalf("NewSchedulerController: %v", err)
	}
	if err := controller.ReloadSettings("*/20 * * * *"); err != nil {
		t.Fatalf("ReloadSettings: %v", err)
	}
	if err := controller.ReloadSettings("* * * *"); err == nil {
		t.Fatal("expected invalid cron error")
	}
	controller.mu.Lock()
	now := time.Now().Add(-time.Hour)
	controller.lastCompletedAt = &now
	controller.mu.Unlock()
	next := controller.computeNextRunAt()
	if next.Before(now.Add(minimumCycleGap)) {
		t.Fatalf("next run %v before minimum gap from %v", next, now)
	}
	if formatTime(nil) != nil {
		t.Fatal("formatTime nil should be nil")
	}
	if formatted := formatTime(&now); formatted == nil {
		t.Fatal("formatTime returned nil")
	}
}

func TestSchedulerStartStopAndAlreadyRunning(t *testing.T) {
	runner := &fakeCycleRunner{schedule: "*/30 * * * *"}
	controller, err := NewSchedulerController(runner)
	if err != nil {
		t.Fatalf("NewSchedulerController: %v", err)
	}
	controller.mu.Lock()
	controller.state = "running"
	controller.mu.Unlock()
	if got := controller.RequestRunNow(); got != "already_running" {
		t.Fatalf("RequestRunNow running = %q", got)
	}
	controller.mu.Lock()
	controller.state = "idle"
	controller.mu.Unlock()
	controller.Start()
	controller.Stop()
	if snap := controller.Snapshot(); snap["state"] != "stopped" {
		t.Fatalf("stopped snapshot = %+v", snap)
	}
}
