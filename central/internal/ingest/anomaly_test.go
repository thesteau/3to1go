package ingest

import (
	"context"
	"strings"
	"testing"

	"github.com/3to1go/shared/anomaly"
)

func sizeCheckService(t *testing.T, mode string, history ...int64) (*Service, *mockIndex) {
	t.Helper()
	svc := newTestService(t)
	svc.settings.AnomalyMode = mode
	index := &mockIndex{archiveSizes: map[string][]int64{"edge/inst/job": history}}
	svc.index = index
	return svc, index
}

func TestArchiveSizeCheckFlagsSharpChanges(t *testing.T) {
	usual := []int64{1_000_000, 1_020_000, 990_000, 1_010_000, 1_005_000, 998_000}
	cases := []struct {
		size    int64
		unusual bool
	}{
		{1_030_000, false},
		{40_000, true},
		{9_000_000, true},
	}
	for _, c := range cases {
		svc, _ := sizeCheckService(t, anomaly.ModeAlert, usual...)
		got := svc.checkArchiveSize(context.Background(), "edge/inst/job", c.size)
		if (got != "") != c.unusual {
			t.Errorf("size %d: %q, want unusual=%v", c.size, got, c.unusual)
		}
	}
	svc, _ := sizeCheckService(t, anomaly.ModeAlert, usual...)
	got := svc.checkArchiveSize(context.Background(), "edge/inst/job", 40_000)
	// "Usual" is the median on a log scale: between 1,000,000 and 1,005,000 bytes here.
	if !strings.Contains(got, "This archive is 39.1 KB, but this job's archives are usually about 979.0 KB.") {
		t.Errorf("message = %q", got)
	}
}

func TestArchiveSizeCheckRecordsHistoryEvenWhenOff(t *testing.T) {
	svc, index := sizeCheckService(t, anomaly.ModeOff, 1_000_000, 1_000_000, 1_000_000, 1_000_000, 1_000_000, 1_000_000)
	if got := svc.checkArchiveSize(context.Background(), "edge/inst/job", 10); got != "" {
		t.Errorf("alerts off but got %q", got)
	}
	if sizes := index.archiveSizes["edge/inst/job"]; len(sizes) != 7 || sizes[6] != 10 {
		t.Errorf("history = %v", sizes)
	}
}

func TestArchiveSizeCheckNeedsHistory(t *testing.T) {
	svc, index := sizeCheckService(t, anomaly.ModeAlert, 1_000_000, 1_000_000)
	if got := svc.checkArchiveSize(context.Background(), "edge/inst/job", 10); got != "" {
		t.Errorf("flagged with only two earlier uploads: %q", got)
	}
	for i := 0; i < 30; i++ {
		svc.checkArchiveSize(context.Background(), "edge/inst/job", 1_000_000)
	}
	if n := len(index.archiveSizes["edge/inst/job"]); n != anomaly.MaxHistory {
		t.Errorf("history kept %d sizes, want %d", n, anomaly.MaxHistory)
	}
}
