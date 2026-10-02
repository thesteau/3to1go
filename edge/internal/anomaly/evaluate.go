package anomaly

import (
	"fmt"
	"math"
	"strings"

	"github.com/3to1go/shared/anomaly"
)

const (
	// sizeFactor: counts and sizes must also change at least this much.
	sizeFactor = 2.0
	// minFilesToCompare: churn and file-type checks need enough files to mean anything.
	minFilesToCompare = 20
	// minCompressibleBytes: tiny jobs compress unpredictably.
	minCompressibleBytes = 1 << 20
)

// Finding is one reason a backup looks unusual.
type Finding struct {
	Signal string `json:"signal"`
	Detail string `json:"detail"`
}

// Result is the outcome of checking one backup against its job's history.
type Result struct {
	Findings []Finding `json:"findings"`
}

// Unusual reports whether any check fired.
func (r Result) Unusual() bool { return len(r.Findings) > 0 }

// Summary joins the findings into one human-readable message.
func (r Result) Summary() string {
	details := make([]string, len(r.Findings))
	for i, f := range r.Findings {
		details[i] = f.Detail
	}
	return strings.Join(details, " ")
}

// Evaluate checks a new backup against the job's accepted history, oldest
// first. Size checks wait for anomaly.MinHistory backups; checks against the
// previous backup run as soon as there is one.
func Evaluate(history []Observation, current Observation) Result {
	var result Result
	add := func(signal, format string, args ...any) {
		result.Findings = append(result.Findings, Finding{Signal: signal, Detail: fmt.Sprintf(format, args...)})
	}

	if len(history) >= anomaly.MinHistory {
		counts := make([]float64, len(history))
		sizes := make([]float64, len(history))
		for i, h := range history {
			counts[i] = float64(h.FileCount)
			sizes[i] = float64(h.SourceBytes)
		}
		if shift := anomaly.LogShift(counts, float64(current.FileCount)); shift.Unusual(sizeFactor) {
			add("file_count", "The folder has %s files, but usually has about %s.", formatCount(float64(current.FileCount)), formatCount(shift.Typical))
		}
		if shift := anomaly.LogShift(sizes, float64(current.SourceBytes)); shift.Unusual(sizeFactor) {
			add("size", "The folder holds %s, but usually holds about %s.", formatBytes(float64(current.SourceBytes)), formatBytes(shift.Typical))
		}
	}

	if finding, ok := checkCompression(history, current); ok {
		result.Findings = append(result.Findings, finding)
	}

	if len(history) > 0 {
		previous := history[len(history)-1]
		if finding, ok := checkChurn(previous, current); ok {
			result.Findings = append(result.Findings, finding)
		}
		if finding, ok := checkFileTypes(previous, current); ok {
			result.Findings = append(result.Findings, finding)
		}
	}
	return result
}

// checkCompression flags archives that stopped compressing. Encrypted data is
// random-looking and incompressible, so ransomware pushes the ratio towards 1.
// Jobs that never compressed well, such as photo folders, are left alone.
func checkCompression(history []Observation, current Observation) (Finding, bool) {
	if current.SourceBytes < minCompressibleBytes {
		return Finding{}, false
	}
	var ratios []float64
	for _, h := range history {
		if h.SourceBytes >= minCompressibleBytes {
			ratios = append(ratios, h.CompressionRatio())
		}
	}
	if len(ratios) < 3 {
		return Finding{}, false
	}
	typical := anomaly.Median(ratios)
	ratio := current.CompressionRatio()
	if ratio < 0.9 || typical > 0.7 || ratio-typical < 0.25 {
		return Finding{}, false
	}
	return Finding{Signal: "compression", Detail: fmt.Sprintf(
		"The archive barely compresses (%.0f%% of the original size, usually %.0f%%), which is typical of encrypted files.",
		ratio*100, typical*100)}, true
}

// checkChurn flags backups where most files changed while the file count
// stayed about the same: the pattern of mass renaming or rewriting in place.
// Adding many new files changes the count too, so it isn't flagged here.
func checkChurn(previous, current Observation) (Finding, bool) {
	if !comparableCounts(previous, current) {
		return Finding{}, false
	}
	similarity := Similarity(previous, current)
	if similarity >= 0.25 {
		return Finding{}, false
	}
	return Finding{Signal: "churn", Detail: fmt.Sprintf(
		"Only about %.0f%% of files are unchanged since the last backup, though the file count barely moved. That looks like mass renaming or rewriting.",
		similarity*100)}, true
}

// checkFileTypes flags a file type that suddenly dominates, such as the new
// extension ransomware adds. It measures the change in the type mix with the
// Jensen-Shannon divergence and names the type that grew most. Like churn, it
// needs the file count to hold steady: ransomware replaces files, while an
// import of new files grows the count and is left to the count check.
func checkFileTypes(previous, current Observation) (Finding, bool) {
	if !comparableCounts(previous, current) {
		return Finding{}, false
	}
	before := shares(previous.Extensions)
	after := shares(current.Extensions)
	grown, gain := "", 0.0
	for ext, share := range after {
		if ext == otherExtension {
			continue
		}
		if d := share - before[ext]; d > gain {
			grown, gain = ext, d
		}
	}
	if grown == "" || gain < 0.25 || before[grown] > 0.05 || jensenShannon(before, after) < 0.3 {
		return Finding{}, false
	}
	return Finding{Signal: "file_types", Detail: fmt.Sprintf(
		"Files ending in %s went from %.0f%% to %.0f%% of the folder.", grown, before[grown]*100, after[grown]*100)}, true
}

// comparableCounts reports whether both backups have enough files to compare
// and the count stayed within about a third of where it was.
func comparableCounts(previous, current Observation) bool {
	if previous.FileCount < minFilesToCompare || current.FileCount < minFilesToCompare {
		return false
	}
	ratio := float64(current.FileCount) / float64(previous.FileCount)
	return ratio >= 0.75 && ratio <= 1.33
}

func shares(counts map[string]int) map[string]float64 {
	total := 0
	for _, n := range counts {
		total += n
	}
	out := make(map[string]float64, len(counts))
	if total == 0 {
		return out
	}
	for ext, n := range counts {
		out[ext] = float64(n) / float64(total)
	}
	return out
}

// jensenShannon returns the Jensen-Shannon divergence in bits, from 0 for
// identical distributions to 1 for distributions with nothing in common.
func jensenShannon(p, q map[string]float64) float64 {
	keys := map[string]bool{}
	for k := range p {
		keys[k] = true
	}
	for k := range q {
		keys[k] = true
	}
	divergence := 0.0
	for k := range keys {
		m := (p[k] + q[k]) / 2
		if p[k] > 0 {
			divergence += p[k] * math.Log2(p[k]/m) / 2
		}
		if q[k] > 0 {
			divergence += q[k] * math.Log2(q[k]/m) / 2
		}
	}
	return divergence
}

func formatCount(n float64) string {
	s := fmt.Sprintf("%.0f", n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func formatBytes(n float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB"}
	i := 0
	for n >= 1024 && i < len(units)-1 {
		n /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s", n, units[i])
	}
	return fmt.Sprintf("%.1f %s", n, units[i])
}
