// Package anomaly spots backups that look unlike a job's history, such as a
// folder that suddenly emptied or files that ransomware has encrypted. It only
// uses file paths, sizes, and archive sizes; it never reads file contents.
package anomaly

import (
	"cmp"
	"hash/fnv"
	"math"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/3to1go/scout/internal/backup"
)

// signatureSize is the number of MinHash slots. 64 estimates the share of
// unchanged files to within about ±6%, in 512 bytes per job.
const signatureSize = 64

// maxExtensions bounds the stored file-type histogram; rarer types are pooled.
const maxExtensions = 32

const (
	noExtension    = "(none)"
	otherExtension = "(other)"
)

// Observation summarizes one backup of a job.
type Observation struct {
	ObservedAt   string         `json:"observed_at"`
	FileCount    int            `json:"file_count"`
	SourceBytes  int64          `json:"source_bytes"`
	ArchiveBytes int64          `json:"archive_bytes"`
	Extensions   map[string]int `json:"extensions"`
	Signature    []uint64       `json:"signature"`
}

// CompressionRatio is the archive's size relative to the files it holds.
func (o Observation) CompressionRatio() float64 {
	if o.SourceBytes <= 0 {
		return 0
	}
	return float64(o.ArchiveBytes) / float64(o.SourceBytes)
}

// Observe summarizes a job's file list and its finished archive.
func Observe(files []*backup.DiscoveredFile, archiveBytes int64, now time.Time) Observation {
	obs := Observation{
		ObservedAt:   now.UTC().Format(time.RFC3339),
		ArchiveBytes: archiveBytes,
		Signature:    make([]uint64, signatureSize),
	}
	for i := range obs.Signature {
		obs.Signature[i] = math.MaxUint64
	}
	extensions := map[string]int{}
	for _, f := range files {
		// Empty folders are archived but say nothing about how the data changed.
		if f.IsDir {
			continue
		}
		obs.FileCount++
		obs.SourceBytes += f.Size
		extensions[extensionOf(f.ArchivePath)]++
		// A file counts as unchanged only if both its path and size match.
		addToSignature(obs.Signature, f.ArchivePath+"\x00"+strconv.FormatInt(f.Size, 10))
	}
	obs.Extensions = topExtensions(extensions)
	return obs
}

func extensionOf(archivePath string) string {
	ext := strings.ToLower(path.Ext(archivePath))
	if ext == "" || ext == "." {
		return noExtension
	}
	return ext
}

func topExtensions(counts map[string]int) map[string]int {
	if len(counts) <= maxExtensions {
		return counts
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	slices.SortFunc(names, func(a, b string) int {
		return cmp.Or(cmp.Compare(counts[b], counts[a]), cmp.Compare(a, b))
	})
	top := make(map[string]int, maxExtensions)
	for i, name := range names {
		if i < maxExtensions-1 {
			top[name] = counts[name]
		} else {
			top[otherExtension] += counts[name]
		}
	}
	return top
}

// addToSignature updates a MinHash signature: each slot keeps the smallest
// hash seen under its own hash function. Two signatures then agree in a share
// of slots that estimates the Jaccard similarity of their underlying sets.
func addToSignature(signature []uint64, token string) {
	h := fnv.New64a()
	h.Write([]byte(token))
	base := h.Sum64()
	for i := range signature {
		if v := mix(base ^ seeds[i]); v < signature[i] {
			signature[i] = v
		}
	}
}

var seeds = func() [signatureSize]uint64 {
	var s [signatureSize]uint64
	for i := range s {
		s[i] = mix(uint64(i) + 1)
	}
	return s
}()

// mix is the splitmix64 finalizer, a fast way to derive independent hashes.
func mix(x uint64) uint64 {
	x += 0x9e3779b97f4a7c15
	x = (x ^ (x >> 30)) * 0xbf58476d1ce4e5b9
	x = (x ^ (x >> 27)) * 0x94d049bb133111eb
	return x ^ (x >> 31)
}

// Similarity estimates the share of files two observations have in common,
// counting a file as shared only if its path and size both match.
func Similarity(a, b Observation) float64 {
	if len(a.Signature) != signatureSize || len(b.Signature) != signatureSize {
		return 1
	}
	same := 0
	for i := range a.Signature {
		if a.Signature[i] == b.Signature[i] {
			same++
		}
	}
	return float64(same) / signatureSize
}
