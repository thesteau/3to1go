package services

import (
	"cmp"
	"slices"

	"github.com/3to1go/central/internal/storage"
)

type snapshotStore interface {
	List(namespace string) ([]storage.StorageFile, error)
	Delete(namespace, filename string) error
}

// PruneOldSnapshots deletes excess snapshots, keeping the keepLast most recent.
// Returns the number of deleted snapshots.
func PruneOldSnapshots(backend snapshotStore, namespace string, keepLast int) (int, error) {
	files, err := backend.List(namespace)
	if err != nil {
		return 0, err
	}
	if len(files) <= keepLast {
		return 0, nil
	}

	slices.SortFunc(files, func(a, b storage.StorageFile) int {
		if a.Mtime != b.Mtime {
			return cmp.Compare(b.Mtime, a.Mtime)
		}
		return cmp.Compare(b.Filename, a.Filename)
	})

	toDelete := files[keepLast:]
	for _, f := range toDelete {
		if err := backend.Delete(namespace, f.Filename); err != nil {
			return 0, err
		}
	}
	return len(toDelete), nil
}
