package services

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/3to1go/edge/internal/backup"
	"github.com/3to1go/edge/internal/config"
)

// DirectoryService lists, saves, and deletes backup job definitions under the scan root.
type DirectoryService struct {
	settings   *config.Settings
	logger     *slog.Logger
	stateStore *StateStore
}

func NewDirectoryService(settings *config.Settings, logger *slog.Logger, stateStore *StateStore) *DirectoryService {
	return &DirectoryService{settings: settings, logger: logger, stateStore: stateStore}
}

// DirectoryEntry is the JSON representation of a scanned directory.
type DirectoryEntry struct {
	RelativePath    string                 `json:"relative_path"`
	AbsolutePath    string                 `json:"absolute_path"`
	Selected        bool                   `json:"selected"`
	BlockedByParent interface{}            `json:"blocked_by_parent"`
	Config          interface{}            `json:"config"`
	ConfigError     interface{}            `json:"config_error"`
	State           JobState               `json:"state"`
}

// ListDirectories walks the scan root up to max_depth and returns directory info.
func (d *DirectoryService) ListDirectories() ([]DirectoryEntry, error) {
	scanRoot, err := filepath.Abs(d.settings.ScanRoot)
	if err != nil {
		return nil, err
	}

	var entries []DirectoryEntry

	var walk func(dir string, depth int, blockedBy interface{})
	walk = func(dir string, depth int, blockedBy interface{}) {
		if depth > d.settings.MaxDepth {
			return
		}

		relPath := "."
		if dir != scanRoot {
			rel, err := filepath.Rel(scanRoot, dir)
			if err == nil {
				relPath = filepath.ToSlash(rel)
			}
		}

		markerPath := filepath.Join(dir, backup.UploadDirFilename)
		selected := false
		if fi, err := os.Stat(markerPath); err == nil && !fi.IsDir() {
			selected = true
		}

		var cfg interface{}
		var cfgErr interface{}
		if selected {
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

		state := d.stateStore.Get(dir)
		entry := DirectoryEntry{
			RelativePath:    relPath,
			AbsolutePath:    dir,
			Selected:        selected,
			BlockedByParent: blockedBy,
			Config:          cfg,
			ConfigError:     cfgErr,
			State:           state,
		}
		entries = append(entries, entry)

		children, err := readSortedSubdirs(dir)
		if err != nil {
			return
		}

		childBlocked := blockedBy
		if selected {
			childBlocked = relPath
		}
		for _, child := range children {
			walk(child, depth+1, childBlocked)
		}
	}

	walk(scanRoot, 0, nil)
	return entries, nil
}

// SaveJob writes the upload_dir marker for relative_path.
func (d *DirectoryService) SaveJob(relativePath string, payload map[string]interface{}) (DirectoryEntry, error) {
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
	var cfg interface{}
	var cfgErr interface{}
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

	state := d.stateStore.Get(dir)
	blocker := d.findBlockingAncestor(dir)
	var blockedByParent interface{}
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
		State:           state,
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
	sort.Slice(dirs, func(i, j int) bool {
		return strings.ToLower(filepath.Base(dirs[i])) < strings.ToLower(filepath.Base(dirs[j]))
	})
	return dirs, nil
}
