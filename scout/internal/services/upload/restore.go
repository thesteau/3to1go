package upload

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/3to1go/shared/protocol"
)

func (c *UploadClient) restoreRequestsPath(scoutID string) string {
	return fmt.Sprintf("/backup/recovery/%s/%s/requests", url.PathEscape(scoutID), url.PathEscape(c.scoutInstanceID))
}

func (c *UploadClient) ListRestoreRequests(ctx context.Context, scoutID string) ([]protocol.RestoreRequest, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.stationURL+c.restoreRequestsPath(scoutID), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.scoutCredential)
	response, err := c.doRequest(req, "restore_requests")
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()
	var requests []protocol.RestoreRequest
	err = json.NewDecoder(io.LimitReader(response.Body, 4<<20)).Decode(&requests)
	return requests, err
}

func (c *UploadClient) DecideRestoreRequest(ctx context.Context, scoutID, id, status string) error {
	_, err := c.jsonPost(ctx, c.restoreRequestsPath(scoutID)+"/"+url.PathEscape(id), map[string]any{"status": status}, "restore_decision", c.readTimeoutPadding)
	return err
}

func (c *UploadClient) DownloadSnapshotByFilename(ctx context.Context, scoutID, jobName, filename, destPath string) (string, error) {
	path := fmt.Sprintf("/backup/recovery/%s/%s/%s/archive/%s", url.PathEscape(scoutID), url.PathEscape(c.scoutInstanceID), url.PathEscape(jobName), url.PathEscape(filename))
	return c.downloadSnapshot(ctx, path, destPath, nil)
}

func (c *UploadClient) DownloadRestoreRequest(ctx context.Context, scoutID, id, destPath string) (string, error) {
	return c.downloadSnapshot(ctx, c.restoreRequestsPath(scoutID)+"/"+url.PathEscape(id)+"/archive", destPath, nil)
}
