package protocol

// Shared values used by the Scout↔Station HTTP protocol.
const (
	ArchiveFormatTarZst = "tar.zst"

	UploadInitiatePath = "/backup/uploads/initiate"

	StatusOffsetMismatch   = "offset_mismatch"
	StatusChecksumMismatch = "checksum_mismatch"
)

// UploadInitRequest is the metadata exchanged when Scout creates or resumes an
// upload session at Station.
type UploadInitRequest struct {
	ScoutID                  string  `json:"scout_id"`
	ScoutInstanceID          string  `json:"scout_instance_id,omitempty"`
	JobName                  string  `json:"job_name"`
	Fingerprint              string  `json:"fingerprint"`
	Timestamp                string  `json:"timestamp"`
	ArchiveFormat            string  `json:"archive_format"`
	ArchiveSizeBytes         int64   `json:"archive_size_bytes"`
	ArchiveSHA256            string  `json:"archive_sha256"`
	IdempotencyKey           string  `json:"idempotency_key"`
	EncryptionKeyFingerprint *string `json:"encryption_key_fingerprint,omitempty"`
	AdvertisedURL            *string `json:"advertised_url,omitempty"`
}
