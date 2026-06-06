package backup

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

const UploadDirFilename = ".upload_dir"

var safeComponentPattern = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)

// JobDefinition describes a single backup job discovered from a .upload_dir marker.
type JobDefinition struct {
	RootPath        string
	JobName         string
	ExcludePatterns []string
	IncludeHidden   bool
	FollowSymlinks  bool
}

// StateKey is the filesystem path used to key job state.
func (j *JobDefinition) StateKey() string {
	return j.RootPath
}

// DiscoverJobs walks scanRoot up to maxDepth levels looking for .upload_dir markers.
func DiscoverJobs(scanRoot string, maxDepth int, warnf func(string, ...any)) ([]*JobDefinition, error) {
	type entry struct {
		dir   string
		depth int
	}
	queue := []entry{{scanRoot, 0}}
	var jobs []*JobDefinition

	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]

		if cur.depth > maxDepth {
			continue
		}

		entries, err := os.ReadDir(cur.dir)
		if err != nil {
			warnf("skipped_missing path=%s detail=%s", cur.dir, err)
			continue
		}

		var markerFound bool
		for _, e := range entries {
			if e.Name() == UploadDirFilename && !e.IsDir() {
				markerFound = true
				break
			}
		}

		if markerFound {
			markerPath := filepath.Join(cur.dir, UploadDirFilename)
			job, err := LoadJobDefinition(cur.dir, markerPath, warnf)
			if err == nil && job != nil {
				jobs = append(jobs, job)
			}
			continue
		}

		if cur.depth == maxDepth {
			continue
		}

		var children []string
		for _, e := range entries {
			if e.IsDir() {
				children = append(children, filepath.Join(cur.dir, e.Name()))
			}
		}
		slices.SortFunc(children, func(a, b string) int {
			return strings.Compare(strings.ToLower(filepath.Base(a)), strings.ToLower(filepath.Base(b)))
		})
		for _, child := range children {
			queue = append(queue, entry{child, cur.depth + 1})
		}
	}
	return jobs, nil
}

// LoadJobDefinition reads a .upload_dir marker and returns the job, or nil on error.
func LoadJobDefinition(dir, markerPath string, warnf func(string, ...any)) (*JobDefinition, error) {
	payload, err := ReadUploadDirPayload(markerPath)
	if err != nil {
		if warnf != nil {
			warnf("invalid_upload_dir path=%s detail=%s", markerPath, err)
		}
		return nil, err
	}
	job, err := BuildJobDefinition(dir, payload)
	if err != nil {
		if warnf != nil {
			warnf("invalid_upload_dir path=%s detail=%s", markerPath, err)
		}
		return nil, err
	}
	return job, nil
}

// ReadUploadDirPayload reads and parses the YAML content of a .upload_dir file.
func ReadUploadDirPayload(markerPath string) (map[string]any, error) {
	raw, err := os.ReadFile(markerPath)
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(string(raw))
	if content == "" {
		return map[string]any{}, nil
	}
	var payload map[string]any
	if err := yaml.Unmarshal(raw, &payload); err != nil {
		return nil, fmt.Errorf("invalid yaml: %w", err)
	}
	if payload == nil {
		return map[string]any{}, nil
	}
	return payload, nil
}

// BuildJobDefinition constructs a JobDefinition from a directory path and YAML payload.
func BuildJobDefinition(dir string, payload map[string]any) (*JobDefinition, error) {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	jobName := strings.TrimSpace(stringVal(payload["job_name"], filepath.Base(absDir)))
	if !safeComponentPattern.MatchString(jobName) {
		return nil, fmt.Errorf("invalid job_name")
	}

	excludeRaw, _ := payload["exclude"]
	excludePatterns, err := parseStringList(excludeRaw)
	if err != nil {
		return nil, fmt.Errorf("exclude must be a list of strings")
	}

	includeHidden := boolVal(payload["include_hidden"], true)
	followSymlinks := boolVal(payload["follow_symlinks"], false)

	return &JobDefinition{
		RootPath:        absDir,
		JobName:         jobName,
		ExcludePatterns: excludePatterns,
		IncludeHidden:   includeHidden,
		FollowSymlinks:  followSymlinks,
	}, nil
}

// JobDefinitionToPayload converts a JobDefinition to a serializable map.
func JobDefinitionToPayload(job *JobDefinition) map[string]any {
	exclude := make([]string, len(job.ExcludePatterns))
	copy(exclude, job.ExcludePatterns)
	return map[string]any{
		"job_name":        job.JobName,
		"exclude":         exclude,
		"include_hidden":  job.IncludeHidden,
		"follow_symlinks": job.FollowSymlinks,
	}
}

// WriteUploadDir serializes a payload and writes it to directory/.upload_dir.
func WriteUploadDir(dir string, payload map[string]any) error {
	job, err := BuildJobDefinition(dir, payload)
	if err != nil {
		return err
	}
	normalized := JobDefinitionToPayload(job)
	out, err := yaml.Marshal(normalized)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, UploadDirFilename), out, 0o644)
}

// DeleteUploadDir removes the .upload_dir marker from a directory.
func DeleteUploadDir(dir string) error {
	path := filepath.Join(dir, UploadDirFilename)
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func stringVal(v any, def string) string {
	if v == nil {
		return def
	}
	s := strings.TrimSpace(fmt.Sprintf("%v", v))
	if s == "" {
		return def
	}
	return s
}

func boolVal(v any, def bool) bool {
	if v == nil {
		return def
	}
	if b, ok := v.(bool); ok {
		return b
	}
	return def
}

func parseStringList(v any) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	list, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("not a list")
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("list contains non-string item")
		}
		result = append(result, s)
	}
	return result, nil
}
