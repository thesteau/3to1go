package directories

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
	"github.com/3to1go/edge/internal/services/state"
)

// jobStateStore is the persistence interface for per-job upload state.
// *state.StateStore satisfies it.
type jobStateStore interface {
	Get(rootPath string) state.JobState
	Set(rootPath string, s state.JobState) error
	Delete(rootPath string) error
}

// jobDiscoveryTTL bounds how long discovered job paths serve repeated UI loads.
const jobDiscoveryTTL = 30 * time.Second

// DirectoryService lists, saves, and deletes backup job definitions under the scan root.
type DirectoryService struct {
	settings   *config.Settings
	logger     *slog.Logger
	stateStore jobStateStore

	jobsMu    sync.Mutex
	jobDirs   []string
	jobDirsAt time.Time
}

func NewDirectoryService(settings *config.Settings, logger *slog.Logger, stateStore jobStateStore) *DirectoryService {
	return &DirectoryService{settings: settings, logger: logger, stateStore: stateStore}
}

// DirectoryEntry is the JSON representation of a scanned directory.
type DirectoryEntry struct {
	RelativePath    string         `json:"relative_path"`
	AbsolutePath    string         `json:"absolute_path"`
	Selected        bool           `json:"selected"`
	BlockedByParent any            `json:"blocked_by_parent"`
	Excluded        bool           `json:"excluded"`
	Config          any            `json:"config"`
	ConfigError     any            `json:"config_error"`
	State           state.JobState `json:"state"`
}

// DirectoryNode is one folder of the lazily loaded directory tree.
type DirectoryNode struct {
	DirectoryEntry
	ChildCount       int `json:"child_count"`
	HiddenChildCount int `json:"hidden_child_count"`
}

// ListJobs returns every directory holding a job marker within max_depth. Discovery
// stops at each marker, so job contents are never walked, and the discovered paths
// are reused briefly across UI polls; job config and state are always read fresh.
func (d *DirectoryService) ListJobs() ([]DirectoryEntry, error) {
	dirs, err := d.jobDirectories()
	if err != nil {
		return nil, err
	}
	entries := make([]DirectoryEntry, 0, len(dirs))
	for _, dir := range dirs {
		entry, err := d.serializeDirectory(dir)
		// A marker removed outside the UI drops out until the next discovery.
		if err != nil || !entry.Selected {
			continue
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (d *DirectoryService) jobDirectories() ([]string, error) {
	d.jobsMu.Lock()
	defer d.jobsMu.Unlock()
	if d.jobDirs != nil && time.Since(d.jobDirsAt) < jobDiscoveryTTL {
		return d.jobDirs, nil
	}
	scanRoot, err := filepath.Abs(d.settings.ScanRoot)
	if err != nil {
		return nil, err
	}
	dirs := []string{}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		var children []string
		for _, e := range entries {
			if e.Name() == backup.UploadDirFilename && !e.IsDir() {
				dirs = append(dirs, dir)
				return
			}
			if e.IsDir() {
				children = append(children, filepath.Join(dir, e.Name()))
			}
		}
		if depth >= d.settings.MaxDepth {
			return
		}
		sortPaths(children)
		for _, child := range children {
			walk(child, depth+1)
		}
	}
	walk(scanRoot, 0)
	d.jobDirs, d.jobDirsAt = dirs, time.Now()
	return dirs, nil
}

func (d *DirectoryService) invalidateJobs() {
	d.jobsMu.Lock()
	d.jobDirs = nil
	d.jobsMu.Unlock()
}

// ListChildren returns the folders directly below relativePath, within max_depth.
// Symlinks are resolved first so a link inside the root cannot list folders outside it.
func (d *DirectoryService) ListChildren(relativePath string) ([]DirectoryNode, error) {
	scanRoot, dir, err := d.resolveBrowsePath(relativePath)
	if err != nil {
		return nil, err
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		return nil, fmt.Errorf("directory not found")
	}
	depth := 0
	if rel, err := filepath.Rel(scanRoot, dir); err == nil && rel != "." {
		depth = len(strings.Split(filepath.ToSlash(rel), "/"))
	}
	nodes := []DirectoryNode{}
	if depth >= d.settings.MaxDepth {
		return nodes, nil
	}
	children, err := readSortedSubdirs(dir)
	if err != nil {
		return nil, err
	}
	owner, blockedBy := d.nearestJob(scanRoot, dir)
	for _, child := range children {
		node := DirectoryNode{DirectoryEntry: d.describeDirectory(scanRoot, child, owner, blockedBy)}
		if depth+1 < d.settings.MaxDepth {
			node.ChildCount, node.HiddenChildCount = countSubdirs(child)
		}
		nodes = append(nodes, node)
	}
	return nodes, nil
}

// nearestJob finds the closest marked directory at or above dir: its job owns
// exclusions below it, and its path blocks nested jobs.
func (d *DirectoryService) nearestJob(scanRoot, dir string) (*backup.JobDefinition, any) {
	var owner *backup.JobDefinition
	var blockedBy any
	current := scanRoot
	rel, _ := filepath.Rel(scanRoot, dir)
	parts := []string{}
	if rel != "." {
		parts = strings.Split(filepath.ToSlash(rel), "/")
	}
	for i := 0; ; i++ {
		markerPath := filepath.Join(current, backup.UploadDirFilename)
		if fi, err := os.Stat(markerPath); err == nil && !fi.IsDir() {
			blockedBy = relativeTo(scanRoot, current)
			if payload, err := backup.ReadUploadDirPayload(markerPath); err == nil {
				if job, err := backup.BuildJobDefinition(current, payload); err == nil {
					owner = job
				}
			}
		}
		if i >= len(parts) {
			return owner, blockedBy
		}
		current = filepath.Join(current, parts[i])
	}
}

// describeDirectory reports one directory given its nearest ancestor job.
func (d *DirectoryService) describeDirectory(scanRoot, dir string, owner *backup.JobDefinition, blockedBy any) DirectoryEntry {
	excluded := false
	if owner != nil {
		if jobRel, err := filepath.Rel(owner.RootPath, dir); err == nil {
			excluded = backup.ExcludedByJob(owner, filepath.ToSlash(jobRel))
		}
	}

	markerPath := filepath.Join(dir, backup.UploadDirFilename)
	selected := false
	var cfg any
	var cfgErr any
	if fi, err := os.Stat(markerPath); err == nil && !fi.IsDir() {
		selected = true
		payload, err := backup.ReadUploadDirPayload(markerPath)
		if err == nil {
			job, err := backup.BuildJobDefinition(dir, payload)
			if err == nil {
				cfg = backup.JobDefinitionToPayload(job)
			} else {
				cfgErr = err.Error()
			}
		} else {
			cfgErr = err.Error()
		}
	}

	return DirectoryEntry{
		RelativePath:    relativeTo(scanRoot, dir),
		AbsolutePath:    dir,
		Selected:        selected,
		BlockedByParent: blockedBy,
		Excluded:        excluded,
		Config:          cfg,
		ConfigError:     cfgErr,
		State:           d.stateStore.Get(dir),
	}
}

func relativeTo(scanRoot, dir string) string {
	if dir == scanRoot {
		return "."
	}
	rel, err := filepath.Rel(scanRoot, dir)
	if err != nil {
		return "."
	}
	return filepath.ToSlash(rel)
}

// countSubdirs counts dir's subdirectories, and separately those that are hidden.
func countSubdirs(dir string) (int, int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0
	}
	total, hidden := 0, 0
	for _, e := range entries {
		if e.IsDir() {
			total++
			if strings.HasPrefix(e.Name(), ".") {
				hidden++
			}
		}
	}
	return total, hidden
}

// SaveJob writes the upload_dir marker for relative_path.
func (d *DirectoryService) SaveJob(relativePath string, payload map[string]any) (DirectoryEntry, error) {
	dir, err := d.resolveDirectory(relativePath)
	if err != nil {
		return DirectoryEntry{}, err
	}

	markerPath := filepath.Join(dir, backup.UploadDirFilename)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		if blocker := d.findBlockingAncestor(dir); blocker != "" {
			return DirectoryEntry{}, fmt.Errorf("directory is nested under existing job %s", blocker)
		}
	}

	if err := backup.WriteUploadDir(dir, payload); err != nil {
		return DirectoryEntry{}, err
	}
	d.invalidateJobs()
	raw, err := backup.ReadUploadDirPayload(markerPath)
	if err != nil {
		return DirectoryEntry{}, err
	}
	job, err := backup.BuildJobDefinition(dir, raw)
	if err != nil {
		return DirectoryEntry{}, err
	}
	d.logger.Info("ui_job_saved", "path", dir, "job_name", job.JobName)
	return d.serializeDirectory(dir)
}

// DeleteJob removes the upload_dir marker and clears state for relative_path.
func (d *DirectoryService) DeleteJob(relativePath string) error {
	dir, err := d.resolveDirectory(relativePath)
	if err != nil {
		return err
	}
	if err := backup.DeleteUploadDir(dir); err != nil {
		return err
	}
	d.invalidateJobs()
	d.stateStore.Delete(dir)
	d.logger.Info("ui_job_deleted", "path", dir)
	return nil
}

// LoadJob returns the JobDefinition for relative_path (error if not configured).
func (d *DirectoryService) LoadJob(relativePath string) (*backup.JobDefinition, error) {
	dir, err := d.resolveDirectory(relativePath)
	if err != nil {
		return nil, err
	}
	markerPath := filepath.Join(dir, backup.UploadDirFilename)
	if _, err := os.Stat(markerPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("job not found")
	}
	payload, err := backup.ReadUploadDirPayload(markerPath)
	if err != nil {
		return nil, err
	}
	return backup.BuildJobDefinition(dir, payload)
}

func (d *DirectoryService) serializeDirectory(dir string) (DirectoryEntry, error) {
	scanRoot, _ := filepath.Abs(d.settings.ScanRoot)
	relPath := "."
	if dir != scanRoot {
		rel, err := filepath.Rel(scanRoot, dir)
		if err == nil {
			relPath = filepath.ToSlash(rel)
		}
	}

	markerPath := filepath.Join(dir, backup.UploadDirFilename)
	selected := false
	var cfg any
	var cfgErr any
	if fi, err := os.Stat(markerPath); err == nil && !fi.IsDir() {
		selected = true
		payload, err := backup.ReadUploadDirPayload(markerPath)
		if err == nil {
			job, err := backup.BuildJobDefinition(dir, payload)
			if err == nil {
				cfg = backup.JobDefinitionToPayload(job)
			} else {
				cfgErr = err.Error()
			}
		} else {
			cfgErr = err.Error()
		}
	}

	s := d.stateStore.Get(dir)
	blocker := d.findBlockingAncestor(dir)
	var blockedByParent any
	if blocker != "" {
		blockedByParent = blocker
	}
	return DirectoryEntry{
		RelativePath:    relPath,
		AbsolutePath:    dir,
		Selected:        selected,
		BlockedByParent: blockedByParent,
		Config:          cfg,
		ConfigError:     cfgErr,
		State:           s,
	}, nil
}

func (d *DirectoryService) resolveDirectory(relativePath string) (string, error) {
	scanRoot, err := filepath.Abs(d.settings.ScanRoot)
	if err != nil {
		return "", err
	}
	var candidate string
	if relativePath == "." || relativePath == "" {
		candidate = scanRoot
	} else {
		candidate = filepath.Join(scanRoot, filepath.FromSlash(relativePath))
	}
	candidate, err = filepath.Abs(candidate)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(candidate+string(filepath.Separator), scanRoot+string(filepath.Separator)) && candidate != scanRoot {
		return "", fmt.Errorf("path must remain within scan root")
	}
	fi, err := os.Stat(candidate)
	if err != nil || !fi.IsDir() {
		return "", fmt.Errorf("directory not found")
	}
	return candidate, nil
}

func (d *DirectoryService) findBlockingAncestor(dir string) string {
	scanRoot, _ := filepath.Abs(d.settings.ScanRoot)
	if dir == scanRoot {
		return ""
	}
	current := filepath.Dir(dir)
	for {
		if current == filepath.Dir(scanRoot) {
			return ""
		}
		markerPath := filepath.Join(current, backup.UploadDirFilename)
		if fi, err := os.Stat(markerPath); err == nil && !fi.IsDir() && current != dir {
			if current == scanRoot {
				return "."
			}
			rel, err := filepath.Rel(scanRoot, current)
			if err == nil {
				return filepath.ToSlash(rel)
			}
		}
		if current == scanRoot {
			return ""
		}
		current = filepath.Dir(current)
	}
}

func readSortedSubdirs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join(dir, e.Name()))
		}
	}
	sortPaths(dirs)
	return dirs, nil
}

func sortPaths(dirs []string) {
	slices.SortFunc(dirs, func(a, b string) int {
		return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
	})
}
