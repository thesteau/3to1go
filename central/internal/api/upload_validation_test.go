package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/central/internal/store"
)

func TestUploadMetadataPaths(t *testing.T) {
	for _, tc := range []struct{ field, value string }{
		{"edge_id", ".."}, {"edge_instance_id", "."}, {"job_name", ".."},
		{"timestamp", "x/../../escape"}, {"timestamp", "not-a-time"},
		{"fingerprint", "../bad"}, {"fingerprint", "1234567g"},
		{"timestamp", "2024-01-01T00:00:00Z"},
	} {
		t.Run(tc.field+"="+tc.value, func(t *testing.T) {
			app := newTestApp(t, nil, &mockCredStore{verifyResult: &store.CredentialRecord{TokenHash: "h"}}, nil, nil)
			body := map[string]any{
				"edge_id": "edge", "edge_instance_id": "instance", "job_name": "job",
				"timestamp": "2024-01-01T00:00:00Z", "fingerprint": "abcdef12",
				"archive_format": "tar.zst", "archive_size_bytes": 100,
				"archive_sha256": strings.Repeat("a", 64), "idempotency_key": "key",
			}
			body[tc.field] = tc.value
			req := jsonReq("POST", "/backup/uploads/initiate", body)
			req.Header.Set("Authorization", "Bearer token")
			rr := httptest.NewRecorder()
			app.handleInitiateUpload(rr, req)
			want := http.StatusBadRequest
			if tc.value == "2024-01-01T00:00:00Z" {
				want = http.StatusOK
			}
			if rr.Code != want {
				t.Fatalf("status %d, want %d: %s", rr.Code, want, rr.Body.String())
			}
		})
	}
}
