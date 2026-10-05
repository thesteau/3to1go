package backup

import (
	"archive/tar"
	"cmp"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/3to1go/scout/internal/cancelio"
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
	return CreateArchiveContext(context.Background(), archivePath, files)
}

func CreateArchiveContext(ctx context.Context, archivePath string, files []*DiscoveredFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
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
	// Error paths only; the success path checks Close below.
	defer func() { _ = f.Close() }()

	enc, err := zstd.NewWriter(cancelio.Writer{Context: ctx, Writer: f}, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if err != nil {
		return err
	}

	tw := tar.NewWriter(enc)
	for _, file := range sorted {
		if err := addFileToTarContext(ctx, tw, file); err != nil {
			_ = enc.Close()
			return err
		}
	}
	if err := tw.Close(); err != nil {
		_ = enc.Close()
		return err
	}
	if err := enc.Close(); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	return f.Close()
}

func addFileToTarContext(ctx context.Context, tw *tar.Writer, file *DiscoveredFile) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if file.IsDir {
		return tw.WriteHeader(&tar.Header{
			Typeflag: tar.TypeDir,
			Name:     file.ArchivePath + "/",
			ModTime:  time.Unix(0, file.MtimeNs),
			Mode:     0o755,
			Format:   tar.FormatPAX,
		})
	}
	src, err := os.Open(file.SourcePath)
	if err != nil {
		return err
	}
	defer func() { _ = src.Close() }()

	// Re-stat after open to get the actual current size; the size recorded at
	// scan time may be stale if the file changed (e.g. an active SQLite DB).
	info, err := src.Stat()
	if err != nil {
		return err
	}
	size := info.Size()

	hdr := &tar.Header{
		Name:    file.ArchivePath,
		Size:    size,
		ModTime: time.Unix(0, file.MtimeNs),
		Mode:    int64(info.Mode().Perm()),
		Uid:     0, Gid: 0,
		Uname:  "",
		Gname:  "",
		Format: tar.FormatPAX,
	}
	if err := tw.WriteHeader(hdr); err != nil {
		return err
	}
	written, err := io.Copy(tw, io.LimitReader(cancelio.Reader{Context: ctx, Reader: src}, size))
	if err != nil {
		return err
	}
	if written < size {
		// File shrank after stat — pad with zeros so the tar entry is consistent.
		_, err = tw.Write(make([]byte, size-written))
	}
	return err
}

// ListArchiveEntries streams the zstd+tar archive and returns a summary of its contents.
func ListArchiveEntries(archivePath, targetRoot string) (map[string]any, error) {
	absTarget, err := filepath.Abs(targetRoot)
	if err != nil {
		return nil, err
	}

	f, err := os.Open(archivePath)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

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

	result := make([]any, len(entries))
	for i, e := range entries {
		result[i] = map[string]any{
			"path":   e.Path,
			"size":   e.Size,
			"mtime":  e.Mtime,
			"action": e.Action,
		}
	}
	return map[string]any{
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
	defer func() { _ = f.Close() }()

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
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeDir {
			return extractedCount, fmt.Errorf("unsupported archive entry: %s", hdr.Name)
		}

		dest, err := archiveDestination(absTarget, hdr.Name)
		if err != nil {
			return extractedCount, err
		}
		if hdr.Typeflag == tar.TypeDir {
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return extractedCount, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return extractedCount, err
		}

		tmp := dest + ".restore.tmp"
		if err := writeAtomic(tmp, dest, hdr.ModTime, tr, os.FileMode(hdr.Mode).Perm()); err != nil {
			_ = os.Remove(tmp)
			return extractedCount, err
		}
		extractedCount++
	}
	return extractedCount, nil
}

func writeAtomic(tmp, dest string, mtime time.Time, r io.Reader, mode os.FileMode) error {
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, r); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
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
