package protocol

// RestoreRequest identifies one archive and the operator's decision about it.
type RestoreRequest struct {
	SourceScoutID    string `json:"source_scout_id,omitempty"`
	SourceInstanceID string `json:"source_instance_id,omitempty"`
	ID               string `json:"id"`
	ScoutID          string `json:"scout_id"`
	ScoutInstanceID  string `json:"scout_instance_id"`
	JobName          string `json:"job_name"`
	Filename         string `json:"filename"`
	Status           string `json:"status"`
	CreatedAt        string `json:"created_at"`
}
