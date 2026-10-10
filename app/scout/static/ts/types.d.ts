// Shapes of the Scout API payloads the UI reads. Fields the server may omit are optional.

interface ScoutSettings {
  scout_id?: string;
  station_url?: string;
  advertised_url?: string;
  scout_credential?: string;
  cron_schedule?: string;
  state_dir?: string;
  spool_dir?: string;
  log_level?: string;
  theme?: string;
  max_depth?: number;
  keep_local_pending?: boolean;
  uploads_paused?: boolean;
  anomaly_mode?: string;
  upload_chunk_size_mb?: number;
  min_upload_chunk_size_mb?: number;
  max_upload_chunk_size_mb?: number;
  upload_retry_max_attempts?: number;
  upload_retry_base_delay_seconds?: number;
  upload_retry_max_delay_seconds?: number;
  upload_connect_timeout_seconds?: number;
  upload_read_timeout_padding_seconds?: number;
  upload_min_throughput_bytes_per_second?: number;
  circuit_breaker_failure_threshold?: number;
  circuit_breaker_cooldown_seconds?: number;
  hook_pre_command?: string;
  hook_post_command?: string;
}

interface SettingsResponse extends ApiBody {
  settings?: ScoutSettings;
}

interface UploadCircuit {
  state?: string;
  consecutive_failures?: number;
  cooldown_remaining_seconds?: number;
}

interface SchedulerStatus {
  state?: string;
}

interface StatusResponse {
  settings?: ScoutSettings;
  settings_status?: { scout_credential_configured?: boolean };
  scheduler?: SchedulerStatus;
  upload_circuit?: UploadCircuit;
  scout_id?: string;
  scout_instance_id?: string;
  scan_root?: string;
  station_url?: string;
  advertised_url?: string;
  cron_schedule?: string;
  encryption_key_fingerprint?: string;
}

interface JobConfig {
  job_name?: string;
  exclude?: string[];
  include_hidden?: boolean;
  follow_symlinks?: boolean;
}

interface JobState {
  last_status?: string;
  pending_archive?: string;
  pending_fingerprint?: string;
  pending_archive_size?: number;
  upload_offset?: number;
  active_phase_percent?: number;
  last_duplicate?: boolean;
  next_retry_at?: string;
  last_upload_started_at?: string;
  last_upload_updated_at?: string;
  last_error_detail?: string;
  last_backup_size_bytes?: number;
}

interface DirectoryEntry {
  relative_path: string;
  absolute_path?: string;
  selected?: boolean;
  excluded?: boolean;
  blocked_by_parent?: string;
  config_error?: string;
  config?: JobConfig;
  state?: JobState;
}

interface DirectoriesResponse {
  directories: DirectoryEntry[];
  discovering?: boolean;
}

interface DirectoryNode extends DirectoryEntry {
  child_count?: number;
  hidden_child_count?: number;
}

interface DirectoryChildrenResponse extends ApiBody {
  directories?: DirectoryNode[];
}

// Status and directory responses load independently and are merged as each arrives.
interface ScoutData extends StatusResponse {
  directories?: DirectoryEntry[];
  jobs_discovering?: boolean;
}

interface EncryptionKeyResponse extends ApiBody {
  key_base64?: string;
  fingerprint?: string;
  new_fingerprint?: string;
}

interface BrowseEntry {
  name: string;
  relative_path: string;
  kind: string;
  size: number;
  reason?: string;
  job_path?: string;
  excluded?: boolean;
}

interface BrowseResponse extends ApiBody {
  entries: BrowseEntry[];
}

interface FolderSizeResponse extends ApiBody {
  size: number;
  files: number;
}

interface OperationResponse extends ApiBody {
  status?: string;
  manual_retry_cleared?: boolean;
  manual_retries_cleared?: number;
}

interface RecoveryPreviewEntry {
  path?: string;
  action?: string;
  size?: number;
}

interface RecoveryResponse extends ApiBody {
  status?: string;
  entries?: RecoveryPreviewEntry[];
  replace_count?: number;
  add_count?: number;
  snapshot_filename?: string;
  snapshot_fingerprint?: string;
  restored_files?: number;
}
