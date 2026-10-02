package anomaly

import (
	"math"
	"testing"
)

func TestMedianAndMAD(t *testing.T) {
	if got := Median([]float64{5, 1, 3}); got != 3 {
		t.Errorf("odd median = %v", got)
	}
	if got := Median([]float64{4, 1, 3, 2}); got != 2.5 {
		t.Errorf("even median = %v", got)
	}
	if got := Median(nil); got != 0 {
		t.Errorf("empty median = %v", got)
	}
	// The median is 4. Deviations from it are 3, 1, 1, 0, 96, whose median is 1:
	// the outlier at 100 doesn't inflate the spread.
	if got := MAD([]float64{1, 5, 3, 4, 100}); got != 1 {
		t.Errorf("MAD = %v", got)
	}
}

func TestLogShiftFlagsLargeMovesOnly(t *testing.T) {
	history := []float64{1000, 1040, 980, 1010, 995, 1020}
	cases := []struct {
		current float64
		unusual bool
	}{
		{1050, false}, // ordinary variation
		{1600, false}, // statistically extreme but under 2x
		{100, true},   // dropped by 90%
		{5000, true},  // grew 5x
	}
	for _, c := range cases {
		shift := LogShift(history, c.current)
		if got := shift.Unusual(2); got != c.unusual {
			t.Errorf("current %v: unusual = %v (z %.1f, ratio %.2f), want %v", c.current, got, shift.Z, shift.Ratio, c.unusual)
		}
	}
}

func TestLogShiftIgnoresAnEarlierOutlier(t *testing.T) {
	history := []float64{1000, 1000, 1000, 1000, 1000, 50}
	shift := LogShift(history, 1000)
	if math.Abs(shift.Typical-1000) > 1e-6 || shift.Unusual(2) {
		t.Errorf("shift = %+v", shift)
	}
}

func TestLogShiftTreatsZeroAsACompleteDrop(t *testing.T) {
	shift := LogShift([]float64{1000, 1040, 980, 1010, 995}, 0)
	if !shift.Unusual(2) || shift.Ratio != 0 || shift.Typical < 990 {
		t.Errorf("zero after real sizes = %+v, want an unusual complete drop", shift)
	}
	if shift := LogShift([]float64{0, 0, 0}, 0); shift.Unusual(2) {
		t.Errorf("zero after only zeros = %+v, want nothing to compare", shift)
	}
	// One positive size among mostly zeros is still a complete drop when the job
	// usually holds data, but not when most accepted backups were empty.
	if shift := LogShift([]float64{1000, 1000, 1000, 0, 0}, 0); !shift.Unusual(2) {
		t.Errorf("zero after mostly real sizes = %+v, want a complete drop", shift)
	}
}

func TestLogShiftAcceptsZeroWhenEmptyIsUsual(t *testing.T) {
	history := make([]float64, 20)
	history[19] = 1000 // 19 approved empty backups and one with data
	if shift := LogShift(history, 0); shift.Unusual(2) {
		t.Errorf("zero after mostly zeros = %+v, want normal", shift)
	}
	if shift := LogShift([]float64{0, 1000}, 0); shift.Unusual(2) {
		t.Errorf("zero after half zeros = %+v, want normal", shift)
	}
}

func TestLogShiftWithoutUsableHistory(t *testing.T) {
	if shift := LogShift(nil, 10); shift.Unusual(2) || shift.Typical != 0 {
		t.Errorf("empty history shift = %+v", shift)
	}
	if shift := LogShift([]float64{0, 0}, 10); shift.Unusual(2) {
		t.Errorf("zero history shift = %+v", shift)
	}
}
