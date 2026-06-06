package backup

import (
	"cmp"
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
)

// ComputeFingerprint returns a SHA-256 hex digest of the sorted path+size manifest.
func ComputeFingerprint(files []*DiscoveredFile) string {
	sorted := make([]*DiscoveredFile, len(files))
	copy(sorted, files)
	slices.SortFunc(sorted, func(a, b *DiscoveredFile) int {
		return cmp.Compare(a.ArchivePath, b.ArchivePath)
	})

	var lines []string
	for _, f := range sorted {
		lines = append(lines, fmt.Sprintf("%s\t%d", f.ArchivePath, f.Size))
	}
	manifest := strings.Join(lines, "\n")
	sum := sha256.Sum256([]byte(manifest))
	return fmt.Sprintf("%x", sum)
}
