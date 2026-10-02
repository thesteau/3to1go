// Package anomaly holds the robust statistics both apps use to spot backups
// that differ sharply from a job's own history.
package anomaly

import (
	"math"
	"slices"
)

// Mode controls what an app does when a backup looks unusual.
const (
	ModeHold  = "hold"  // keep the archive staged until an operator approves it (Edge only)
	ModeAlert = "alert" // notify, but carry on as normal
	ModeOff   = "off"
)

// MinHistory is how many past backups a job needs before size and count
// checks run. Fewer than this gives too little to judge "normal" by.
const MinHistory = 5

// MaxHistory is how many past backups each job remembers.
const MaxHistory = 20

// zThreshold is how many robust standard deviations from the median a value
// must be before it is unusual. It is deliberately high: alerts must be rare.
const zThreshold = 4.0

// minLogSpread stops jobs with near-identical history (a MAD of almost zero)
// from flagging tiny changes. In log space, 0.1 is roughly a 10% change.
const minLogSpread = 0.1

// Median returns the middle value of xs, or 0 for an empty slice.
func Median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	sorted := slices.Clone(xs)
	slices.Sort(sorted)
	mid := len(sorted) / 2
	if len(sorted)%2 == 1 {
		return sorted[mid]
	}
	return (sorted[mid-1] + sorted[mid]) / 2
}

// MAD returns the median absolute deviation of xs from their median.
func MAD(xs []float64) float64 {
	med := Median(xs)
	deviations := make([]float64, len(xs))
	for i, x := range xs {
		deviations[i] = math.Abs(x - med)
	}
	return Median(deviations)
}

// Shift describes how far a positive quantity moved from its history.
type Shift struct {
	Typical float64 // the history's median, in the original units
	Ratio   float64 // current / typical
	Z       float64 // robust z-score in log space
}

// Unusual reports whether the shift is both statistically extreme and large
// in absolute terms: at least factor times bigger or smaller than typical.
func (s Shift) Unusual(factor float64) bool {
	if s.Typical <= 0 {
		return false
	}
	return (s.Z >= zThreshold && s.Ratio >= factor) || (s.Z <= -zThreshold && s.Ratio <= 1/factor)
}

// LogShift compares current against history using the median and MAD of their
// logarithms, which suits sizes and counts that grow multiplicatively. The
// median-based estimates ignore a few earlier outliers, unlike a mean.
func LogShift(history []float64, current float64) Shift {
	logs := make([]float64, 0, len(history))
	for _, v := range history {
		if v > 0 {
			logs = append(logs, math.Log(v))
		}
	}
	if len(logs) == 0 {
		return Shift{}
	}
	med := Median(logs)
	typical := math.Exp(med)
	// Zero has no logarithm, but after a history of real sizes it is the
	// largest possible drop, such as every file truncated to nothing.
	if current <= 0 {
		return Shift{Typical: typical, Ratio: 0, Z: math.Inf(-1)}
	}
	// 1.4826 scales the MAD to match a standard deviation for normal data.
	spread := math.Max(1.4826*MAD(logs), minLogSpread)
	return Shift{Typical: typical, Ratio: current / typical, Z: (math.Log(current) - med) / spread}
}
