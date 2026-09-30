// Shapes of the Central API payloads the UI reads. Fields the server may omit are optional.

type StatusKind = "info" | "success" | "error" | "warning";

interface ApiBody {
  detail?: string;
}

interface CurrentUser {
  id: number;
  username: string;
  is_admin: boolean;
  is_bootstrap_admin?: boolean;
  must_change_password: boolean;
}

interface SessionResponse extends ApiBody {
  authenticated?: boolean;
  user?: CurrentUser | null;
}

interface UserResponse extends ApiBody {
  user: CurrentUser;
}

interface UsersResponse extends ApiBody {
  users?: CurrentUser[];
}

interface CentralSettings {
  retention_keep_last?: number;
  log_level?: string;
  theme?: string;
  max_upload_size_mb?: number;
  upload_chunk_size_mb?: number;
  upload_session_ttl_hours?: number;
  upload_cleanup_interval_seconds?: number;
  snapshot_verify_interval_hours?: number;
  uploads_paused?: boolean;
  ntfy_url?: string;
  ntfy_topic?: string;
  ntfy_message_template?: string;
  ntfy_match_edge_id?: string;
  ntfy_match_edge_instance_id?: string;
  ntfy_match_source?: string;
  hook_pre_command?: string;
  hook_post_command?: string;
}

interface SettingsResponse extends ApiBody {
  settings?: CentralSettings;
}

interface StoredFile {
  name: string;
  size_bytes?: number;
  viewable?: boolean;
}

interface NtfyConfig extends ApiBody {
  ntfy_url?: string;
  ntfy_topic?: string;
  ntfy_message_template?: string;
  ntfy_match_edge_id?: string;
  ntfy_match_edge_instance_id?: string;
  ntfy_match_source?: string;
  default_message_template?: string;
}

interface CertificateConfig extends ApiBody {
  cert_dir?: string;
  files?: StoredFile[];
}

interface HookConfig extends ApiBody {
  script_dir?: string;
  pre_command?: string;
  post_command?: string;
  files?: StoredFile[];
}

interface HookFileResponse extends ApiBody {
  filename?: string;
  content?: string;
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
}

interface SnapshotJob {
  job_name: string;
  snapshot_count?: number;
  snapshots?: Snapshot[];
}

interface EdgeInstance {
  edge_instance_id?: string | null;
  advertised_url?: string;
  credential_configured?: boolean;
  last_upload_tls?: boolean;
  encryption_key_fingerprint?: string;
  jobs?: SnapshotJob[];
}

interface EdgeGroup {
  edge_id: string;
  instances?: EdgeInstance[];
}

interface OverviewResponse {
  settings?: CentralSettings;
  edges?: EdgeGroup[];
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
  __centralSettings?: CentralSettings;
}
