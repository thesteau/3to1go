package directories

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/3to1go/edge/internal/backup"
)

// FolderSize reports source bytes without following symlinks or counting runtime data.
func (d *DirectoryService) FolderSize(ctx context.Context, relativePath string) (map[string]any, error) {
	_, path, err := d.resolveBrowsePath(relativePath)
	if err != nil {
		return nil, err
	}
	var size, files int64
	err = filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() && backup.IsRuntimePath(path) {
			return filepath.SkipDir
		}
		if entry.Type().IsRegular() {
			info, err := entry.Info()
			if err != nil {
				return err
			}
			size += info.Size()
			files++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"size": size, "files": files}, nil
}

type FileEntry struct {
	Name         string `json:"name"`
	RelativePath string `json:"relative_path"`
	Kind         string `json:"kind"`
	Size         int64  `json:"size"`
	JobPath      string `json:"job_path"`
	Excluded     bool   `json:"excluded"`
	Reason       string `json:"reason"`
}

// resolveBrowsePath rejects traversal and symlinks escaping the scan root.
func (d *DirectoryService) resolveBrowsePath(relativePath string) (string, string, error) {
	root, err := filepath.Abs(d.settings.ScanRoot)
	if err != nil {
		return "", "", err
	}
	if relativePath == "" {
		relativePath = "."
	}
	if relativePath != "." && !filepath.IsLocal(filepath.FromSlash(relativePath)) {
		return "", "", fmt.Errorf("path must remain within scan root")
	}
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", "", err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return "", "", err
	}
	rel, err := filepath.Rel(realRoot, realPath)
	if err != nil || (rel != "." && !filepath.IsLocal(rel)) {
		return "", "", fmt.Errorf("path must remain within scan root")
	}
	return root, path, nil
}

// owningJob follows discovery semantics: the first marked ancestor owns a path.
func (d *DirectoryService) owningJob(root, path string) (*backup.JobDefinition, string, error) {
	rel, _ := filepath.Rel(root, path)
	current := root
	parts := strings.Split(filepath.ToSlash(rel), "/")
	for i := 0; ; i++ {
		if i > d.settings.MaxDepth {
			break
		}
		marker := filepath.Join(current, backup.UploadDirFilename)
		if info, err := os.Stat(marker); err == nil && info.Mode().IsRegular() {
			job, err := backup.LoadJobDefinition(current, marker, nil)
			jobRel, _ := filepath.Rel(root, current)
			return job, filepath.ToSlash(jobRel), err
		}
		if i >= len(parts) || rel == "." {
			break
		}
		current = filepath.Join(current, parts[i])
	}
	return nil, "", nil
}

// Browse lists one level on demand, independently of the job discovery depth.
func (d *DirectoryService) Browse(relativePath string) ([]FileEntry, error) {
	root, dir, err := d.resolveBrowsePath(relativePath)
	if err != nil {
		return nil, err
	}
	if backup.IsRuntimePath(dir) {
		return nil, fmt.Errorf("Edge runtime data is excluded automatically")
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	job, jobPath, err := d.owningJob(root, dir)
	if err != nil {
		return nil, err
	}
	result := make([]FileEntry, 0, len(children))
	for _, child := range children {
		info, err := child.Info()
		if err != nil {
			return nil, err
		}
		path := filepath.Join(dir, child.Name())
		rel, _ := filepath.Rel(root, path)
		entry := FileEntry{Name: child.Name(), RelativePath: filepath.ToSlash(rel), Size: info.Size(), Kind: "file", JobPath: jobPath}
		if info.IsDir() {
			entry.Kind = "directory"
		}
		if info.Mode()&os.ModeSymlink != 0 {
			entry.Kind = "symlink"
		}
		if job != nil {
			jobRel, _ := filepath.Rel(job.RootPath, path)
			entry.Excluded = backup.ExcludedByJob(job, filepath.ToSlash(jobRel))
			if entry.Excluded {
				entry.Reason = "Excluded by job settings"
			}
			if entry.Kind == "symlink" && !job.FollowSymlinks {
				entry.Excluded = true
				entry.Reason = "Symlinks are not followed"
			}
		}
		if backup.IsRuntimePath(path) {
			entry.Excluded = true
			entry.Reason = "Edge runtime data"
			entry.JobPath = ""
		}
		if child.Name() == backup.UploadDirFilename {
			entry.JobPath = ""
			entry.Reason = "Backup settings"
		}
		result = append(result, entry)
	}
	slices.SortFunc(result, func(a, b FileEntry) int {
		if a.Kind == "directory" && b.Kind != "directory" {
			return -1
		}
		if b.Kind == "directory" && a.Kind != "directory" {
			return 1
		}
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return result, nil
}

// ExcludePath updates the owning job, never creates a nested job definition.
func (d *DirectoryService) ExcludePath(relativePath string) error {
	root, path, err := d.resolveBrowsePath(relativePath)
	if err != nil {
		return err
	}
	if backup.IsRuntimePath(path) {
		return fmt.Errorf("Edge runtime data is already excluded")
	}
	if filepath.Base(path) == backup.UploadDirFilename {
		return fmt.Errorf("cannot exclude backup settings")
	}
	job, _, err := d.owningJob(root, filepath.Dir(path))
	if err != nil {
		return err
	}
	if job == nil {
		return fmt.Errorf("select a parent folder for backup first")
	}
	rel, err := filepath.Rel(job.RootPath, path)
	if err != nil || !filepath.IsLocal(rel) || rel == "." {
		return fmt.Errorf("cannot exclude the job root")
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	pattern := "/" + filepath.ToSlash(rel)
	if info.IsDir() {
		pattern += "/"
	}
	if slices.Contains(job.ExcludePatterns, pattern) {
		return nil
	}
	// Preserve the existing settings; WriteUploadDir validates the new payload.
	payload, err := backup.ReadUploadDirPayload(filepath.Join(job.RootPath, backup.UploadDirFilename))
	if err != nil {
		return err
	}
	patterns := make([]any, 0, len(job.ExcludePatterns)+1)
	for _, existing := range job.ExcludePatterns {
		patterns = append(patterns, existing)
	}
	payload["exclude"] = append(patterns, pattern)
	return backup.WriteUploadDir(job.RootPath, payload)
}
