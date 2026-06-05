package backup

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

// ComputeFingerprint returns a SHA-256 hex digest of the sorted path+size manifest.
func ComputeFingerprint(files []*DiscoveredFile) string {
	sorted := make([]*DiscoveredFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].ArchivePath < sorted[j].ArchivePath
	})

	var lines []string
	for _, f := range sorted {
		lines = append(lines, fmt.Sprintf("%s\t%d", f.ArchivePath, f.Size))
	}
	manifest := strings.Join(lines, "\n")
	sum := sha256.Sum256([]byte(manifest))
	return fmt.Sprintf("%x", sum)
}
