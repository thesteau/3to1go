// Shapes of the Station API payloads the UI reads. Fields the server may omit are optional.

interface StationSettings {
  retention_keep_last?: number;
  log_level?: string;
  theme?: string;
  max_upload_size_mb?: number;
  upload_chunk_size_mb?: number;
  upload_session_ttl_hours?: number;
  upload_cleanup_interval_seconds?: number;
  snapshot_verify_interval_hours?: number;
  uploads_paused?: boolean;
  anomaly_mode?: string;
  hook_pre_command?: string;
  hook_post_command?: string;
}

interface SettingsResponse extends ApiBody {
  settings?: StationSettings;
}

interface MintCredentialResponse extends ApiBody {
  credential?: string;
  message?: string;
}

interface RevokeCredentialResponse extends ApiBody {
  affected_instances?: string[];
}

interface InstanceDeleteDetail {
  cleanup_available?: boolean;
  message?: string;
}

interface InstanceDeleteResponse {
  detail?: string | InstanceDeleteDetail;
}

interface Snapshot {
  name: string;
  filename?: string;
  size_bytes?: number;
  unusual?: string;
}

interface SnapshotJob {
  job_name: string;
  snapshot_count?: number;
  snapshots?: Snapshot[];
}

interface ScoutInstance {
  scout_instance_id?: string | null;
  advertised_url?: string;
  credential_configured?: boolean;
  last_upload_tls?: boolean;
  encryption_key_fingerprint?: string;
  jobs?: SnapshotJob[];
}

interface ScoutGroup {
  scout_id: string;
  instances?: ScoutInstance[];
}

interface OverviewResponse {
  settings?: StationSettings;
  scouts?: ScoutGroup[];
  backup_dir?: string;
  retention_keep_last?: number;
}

interface StorageOverview {
  status?: string;
  disk_used_bytes?: number;
  disk_free_bytes?: number;
  disk_total_bytes?: number;
}

interface VerifyResult {
  status?: string;
  checked_at?: string;
  failure_count: number;
  total_checked: number;
  last_failure?: string;
  last_failure_msg?: string;
}

interface Window {
  __stationSettings?: StationSettings;
}
