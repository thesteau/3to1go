package backup

import (
	"archive/tar"
	"fmt"
	"io"
	"os"
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"

	securejoin "github.com/cyphar/filepath-securejoin"
	"github.com/klauspost/compress/zstd"
)

// TimestampForFilename formats a UTC time for use in archive filenames.
func TimestampForFilename(t time.Time) string {
	return t.UTC().Format("2006-01-02T15-04-05Z")
}

// TimestampForAPI formats a UTC time for API payloads.
func TimestampForAPI(t time.Time) string {
	return t.UTC().Format("2006-01-02T15:04:05Z")
}

// BuildArchiveName returns the canonical archive filename.
func BuildArchiveName(jobName string, t time.Time, fingerprint string) string {
	fp := fingerprint
	if len(fp) > 8 {
		fp = fp[:8]
	}
	return fmt.Sprintf("%s__%s__%s.tar.zst", jobName, TimestampForFilename(t), fp)
}

// CreateArchive writes a zstd-compressed PAX tar of files to archivePath.
func CreateArchive(archivePath string, files []*DiscoveredFile) error {
	if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
		return err
	}

	sorted := make([]*DiscoveredFile, len(files))
	copy(sorted, files)
	slices.SortFunc(sorted, func(a, b *DiscoveredFile) int {
		return cmp.Compare(a.ArchivePath, b.ArchivePath)
	})

	f, err := os.Create(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()

	enc, err := zstd.NewWriter(f, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return err
	}

	tw := tar.NewWriter(enc)
	for _, file := range sorted {
		if err := addFileToTar(tw, file); err != nil {
			enc.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return nil
}

func addFileToTar(tw *tar.Writer, file *DiscoveredFile) error {
	src, err := os.Open(file.SourcePath)
	if err != nil {
		return err
	}
	defer src.Close()

	hdr := &tar.Header{
		Name:    file.ArchivePath,
		Size:    file.Size,
		ModTime: time.Unix(0, file.MtimeNs),
		Mode:    0o644,
		Uid:     0, Gid: 0,
		Uname:  "",
		Gname:  "",
		Format: tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	_, err = io.Copy(tw, src)
	return err
}

// ListArchiveEntries streams the zstd+tar archive and returns a summary of its contents.
func ListArchiveEntries(archivePath, targetRoot string) (map[string]interface{}, error) {
	absTarget, err := filepath.Abs(targetRoot)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return nil, err
	}
	defer dec.Close()

	type entry struct {
		Path   string  `json:"path"`
		Size   int64   `json:"size"`
		Mtime  float64 `json:"mtime"`
		Action string  `json:"action"`
	}

	var entries []entry
	replaceCount, addCount := 0, 0

	tr := tar.NewReader(dec)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return nil, fmt.Errorf("unsupported archive entry: %s", hdr.Name)
		}
		dest, err := archiveDestination(absTarget, hdr.Name)
		if err != nil {
			return nil, err
		}
		action := "add"
		if _, err := os.Stat(dest); err == nil {
			action = "replace"
			replaceCount++
		} else {
			addCount++
		}
		entries = append(entries, entry{
			Path:   hdr.Name,
			Size:   hdr.Size,
			Mtime:  float64(hdr.ModTime.Unix()),
			Action: action,
		})
	}

	result := make([]interface{}, len(entries))
	for i, e := range entries {
		result[i] = map[string]interface{}{
			"path":   e.Path,
			"size":   e.Size,
			"mtime":  e.Mtime,
			"action": e.Action,
		}
	}
	return map[string]interface{}{
		"entries":       result,
		"total_files":   len(entries),
		"replace_count": replaceCount,
		"add_count":     addCount,
	}, nil
}

// ExtractArchive streams the zstd+tar archive and writes files to targetRoot.
func ExtractArchive(archivePath, targetRoot string) (int, error) {
	absTarget, err := filepath.Abs(targetRoot)
	if err != nil {
		return 0, err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	dec, err := zstd.NewReader(f)
	if err != nil {
		return 0, err
	}
	defer dec.Close()

	extractedCount := 0
	tr := tar.NewReader(dec)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return extractedCount, err
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			return extractedCount, fmt.Errorf("unsupported archive entry: %s", hdr.Name)
		}

		dest, err := archiveDestination(absTarget, hdr.Name)
		if err != nil {
			return extractedCount, err
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return extractedCount, err
		}

		tmp := dest + ".restore.tmp"
		if err := writeAtomic(tmp, dest, hdr.ModTime, tr); err != nil {
			os.Remove(tmp)
			return extractedCount, err
		}
		extractedCount++
	}
	return extractedCount, nil
}

func writeAtomic(tmp, dest string, mtime time.Time, r io.Reader) error {
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	return os.Chtimes(dest, mtime, mtime)
}

func archiveDestination(targetRoot, memberName string) (string, error) {
	cleaned := filepath.Clean(memberName)
	if filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, "..") {
		return "", fmt.Errorf("invalid archive entry: %s", memberName)
	}
	dest, err := securejoin.SecureJoin(targetRoot, cleaned)
	if err != nil {
		return "", fmt.Errorf("invalid archive entry: %s", memberName)
	}
	return dest, nil
}
