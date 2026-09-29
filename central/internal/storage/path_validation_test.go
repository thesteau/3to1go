package storage

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorageRejectsTraversal(t *testing.T) {
	for _, tc := range []struct{ namespace, filename string }{
		{"../outside", "backup.tar.zst"}, {"edge/../job", "backup.tar.zst"},
		{"edge/./job", "backup.tar.zst"}, {"/absolute", "backup.tar.zst"},
		{`edge\..\outside`, "backup.tar.zst"}, {"edge/instance/job", "../outside.tar.zst"},
		{"edge/instance/job", `..\outside.tar.zst`}, {"edge/instance/job", "job__x/../../outside.tar.zst"},
		{"edge/instance/job", "C:outside.tar.zst"},
	} {
		t.Run(tc.namespace+"/"+tc.filename, func(t *testing.T) {
			b, root := newBackend(t)
			staged := filepath.Join(root, "staged.bin")
			writeFile(t, staged, "payload")
			if _, err := b.Store(tc.namespace, tc.filename, staged); err == nil {
				t.Fatal("Store accepted traversal")
			}
			if _, err := os.Stat(staged); err != nil {
				t.Fatalf("staged upload was changed: %v", err)
			}
			if _, err := b.Open(tc.namespace, tc.filename); err == nil {
				t.Fatal("Open accepted traversal")
			}
			if err := b.Delete(tc.namespace, tc.filename); err == nil {
				t.Fatal("Delete accepted traversal")
			}
		})
	}
}
