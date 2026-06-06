package backup

import (
	"cmp"
	"os"
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
}

// BuildFileList walks the job's root directory and returns regular files, applying
// exclude patterns and hidden-file filtering as configured in the job.
func BuildFileList(job *JobDefinition, warnf func(string, ...interface{})) ([]*DiscoveredFile, error) {
	var files []*DiscoveredFile
	stack := []string{job.RootPath}
	visited := make(map[string]bool)

	for len(stack) > 0 {
		curDir := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

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

		for _, entry := range entries {
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

	slices.SortFunc(files, func(a, b *DiscoveredFile) int {
		return cmp.Compare(a.ArchivePath, b.ArchivePath)
	})
	return files, nil
}

func containsHidden(archivePath string) bool {
	for _, part := range strings.Split(archivePath, "/") {
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
		if normalized == "" {
			continue
		}

		if strings.HasSuffix(normalized, "/") {
			prefix := strings.TrimSuffix(normalized, "/")
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
