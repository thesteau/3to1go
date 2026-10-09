package api

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/3to1go/station/internal/ingest"
	"github.com/3to1go/station/internal/store"
)

func TestUploadRejectsWrongOwnerBeforeMutation(t *testing.T) {
	for _, method := range []string{"PUT", "POST"} {
		t.Run(method, func(t *testing.T) {
			a := newTestApp(t, nil, &mockCredStore{verifyResult: &store.CredentialRecord{TokenHash: "wrong-token"}}, nil, nil)
			service := &mockIngest{authErr: &ingest.HTTPError{Code: 403, Message: "Station token does not own this upload"}}
			a.ingest = service
			r := httptest.NewRequest(method, "/backup/uploads/id/chunk?offset=0", strings.NewReader("secret"))
			r.SetPathValue("upload_id", "id")
			r.Header.Set("Authorization", "Bearer valid-but-wrong-token")
			w := httptest.NewRecorder()
			if method == "PUT" {
				a.handleAppendChunk(w, r)
			} else {
				a.handleFinalizeUpload(w, r)
			}
			if w.Code != 403 || service.ownerHash != "wrong-token" || service.chunkCalled || service.finalizeCalled {
				t.Fatalf("unauthorized mutation: %d %+v", w.Code, service)
			}
		})
	}
}
