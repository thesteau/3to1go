package services

import (
	"sort"

	"github.com/relay/central/internal/storage"
)

// PruneOldSnapshots deletes excess snapshots, keeping the keepLast most recent.
// Returns the number of deleted snapshots.
func PruneOldSnapshots(backend *storage.LocalBackend, namespace string, keepLast int) (int, error) {
	files, err := backend.List(namespace)
	if err != nil {
		return 0, err
	}
	if len(files) <= keepLast {
		return 0, nil
	}

	sort.Slice(files, func(i, j int) bool {
		if files[i].Mtime != files[j].Mtime {
			return files[i].Mtime > files[j].Mtime
		}
		return files[i].Filename > files[j].Filename
	})

	toDelete := files[keepLast:]
	for _, f := range toDelete {
		if err := backend.Delete(namespace, f.Filename); err != nil {
			return 0, err
		}
	}
	return len(toDelete), nil
}
