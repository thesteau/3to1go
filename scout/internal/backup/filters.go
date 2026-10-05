package backup

import (
	"cmp"
	"context"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// DiscoveredFile describes a single file collected during a job scan.
type DiscoveredFile struct {
	SourcePath  string
	ArchivePath string // slash-separated path relative to job root
	Size        int64
	MtimeNs     int64
	IsDir       bool // an included folder with nothing else archived inside it
}

// BuildFileList walks the job's root directory and returns regular files, applying
// exclude patterns and hidden-file filtering as configured in the job. Folders
// that would otherwise be lost because nothing inside them is archived are
// returned as IsDir entries so restore can recreate them.
func BuildFileList(job *JobDefinition, warnf func(string, ...any)) ([]*DiscoveredFile, error) {
	return BuildFileListContext(context.Background(), job, warnf)
}

func BuildFileListContext(ctx context.Context, job *JobDefinition, warnf func(string, ...any)) ([]*DiscoveredFile, error) {
	var files, dirs []*DiscoveredFile
	stack := []string{job.RootPath}
	visited := make(map[string]bool)

	for len(stack) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		curDir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if IsRuntimePath(curDir) {
			continue
		}

		visitKey := curDir
		if job.FollowSymlinks {
			if abs, err := filepath.EvalSymlinks(curDir); err == nil {
				visitKey = abs
			}
		}
		if visited[visitKey] {
			continue
		}
		visited[visitKey] = true

		entries, err := os.ReadDir(curDir)
		if err != nil {
			if warnf != nil {
				warnf("skipped_missing path=%s detail=%s", curDir, err)
			}
			continue
		}
		if curDir != job.RootPath {
			if rel, err := filepath.Rel(job.RootPath, curDir); err == nil {
				var mtime int64
				if info, err := os.Stat(curDir); err == nil {
					mtime = info.ModTime().UnixNano()
				}
				dirs = append(dirs, &DiscoveredFile{SourcePath: curDir, ArchivePath: filepath.ToSlash(rel), MtimeNs: mtime, IsDir: true})
			}
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			entryPath := filepath.Join(curDir, entry.Name())
			rel, err := filepath.Rel(job.RootPath, entryPath)
			if err != nil {
				continue
			}
			archivePath := filepath.ToSlash(rel)

			if archivePath == UploadDirFilename {
				continue
			}
			if !job.IncludeHidden && containsHidden(archivePath) {
				continue
			}
			if matchesExclude(archivePath, job.ExcludePatterns) {
				continue
			}

			info, err := entry.Info()
			if err != nil {
				if warnf != nil {
					warnf("skipped_missing path=%s detail=%s", entryPath, err)
				}
				continue
			}

			if entry.Type()&os.ModeSymlink != 0 {
				if job.FollowSymlinks {
					resolved, err := filepath.EvalSymlinks(entryPath)
					if err != nil {
						if warnf != nil {
							warnf("skipped_missing path=%s detail=%s", entryPath, err)
						}
						continue
					}
					if IsRuntimePath(resolved) {
						continue
					}
					info, err = os.Stat(resolved)
					if err != nil {
						if warnf != nil {
							warnf("skipped_missing path=%s detail=%s", entryPath, err)
						}
						continue
					}
					if info.IsDir() {
						stack = append(stack, entryPath)
						continue
					}
				} else {
					continue
				}
			} else if info.IsDir() {
				stack = append(stack, entryPath)
				continue
			}

			if !info.Mode().IsRegular() {
				continue
			}

			files = append(files, &DiscoveredFile{
				SourcePath:  entryPath,
				ArchivePath: archivePath,
				Size:        info.Size(),
				MtimeNs:     info.ModTime().UnixNano(),
			})
		}
	}

	// Only folders with no archived descendants need their own entry; the rest
	// are recreated by the files and folders beneath them.
	covered := make(map[string]bool)
	for _, entry := range slices.Concat(files, dirs) {
		for parent := path.Dir(entry.ArchivePath); parent != "."; parent = path.Dir(parent) {
			covered[parent] = true
		}
	}
	for _, dir := range dirs {
		if !covered[dir.ArchivePath] {
			files = append(files, dir)
		}
	}

	slices.SortFunc(files, func(a, b *DiscoveredFile) int {
		return cmp.Compare(a.ArchivePath, b.ArchivePath)
	})
	return files, nil
}

func containsHidden(archivePath string) bool {
	for part := range strings.SplitSeq(archivePath, "/") {
		if strings.HasPrefix(part, ".") {
			return true
		}
	}
	return false
}

func matchesExclude(archivePath string, patterns []string) bool {
	basename := filepath.Base(archivePath)

	for _, pattern := range patterns {
		normalized := strings.TrimSpace(pattern)
		if strings.HasPrefix(pattern, "/") {
			normalized = pattern
		}
		if normalized == "" {
			continue
		}
		// Browser-generated paths are rooted at the job and literal, so names
		// containing glob characters and same-named siblings stay unambiguous.
		if strings.HasPrefix(normalized, "/") {
			exact := strings.TrimPrefix(normalized, "/")
			if prefix, directory := strings.CutSuffix(exact, "/"); directory {
				if archivePath == prefix || strings.HasPrefix(archivePath, prefix+"/") {
					return true
				}
			} else if archivePath == exact {
				return true
			}
			continue
		}

		if before, ok := strings.CutSuffix(normalized, "/"); ok {
			prefix := before
			if archivePath == prefix ||
				strings.HasPrefix(archivePath, prefix+"/") ||
				strings.Contains(archivePath, "/"+prefix+"/") {
				return true
			}
			continue
		}

		if strings.Contains(normalized, "/") {
			if matched, _ := filepath.Match(normalized, archivePath); matched {
				return true
			}
			continue
		}

		if matched, _ := filepath.Match(normalized, archivePath); matched {
			return true
		}
		if matched, _ := filepath.Match(normalized, basename); matched {
			return true
		}
	}
	return false
}

// ExcludedByJob also checks ancestors, matching the scanner's directory pruning.
func ExcludedByJob(job *JobDefinition, relativePath string) bool {
	if !job.IncludeHidden && containsHidden(relativePath) {
		return true
	}
	for path := relativePath; path != "."; path = filepath.ToSlash(filepath.Dir(path)) {
		if matchesExclude(path, job.ExcludePatterns) {
			return true
		}
	}
	return false
}
