// API and UI types shared by both applications.

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

interface BuildInfo {
  version?: string;
  commit?: string;
  summary?: string;
}

interface UsersResponse extends ApiBody {
  users?: CurrentUser[];
  build?: BuildInfo;
}

interface StoredFile {
  name: string;
  size_bytes?: number;
  viewable?: boolean;
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

interface IntegrationDestination {
  id: string;
  name: string;
  enabled: boolean;
  format: string;
  events: string[];
  match_scout_id: string;
  match_instance_id: string;
  match_job_name: string;
  match_source_address: string;
  message_template: string;
  include_detail: boolean;
  timeout_seconds: number;
  url_configured?: boolean;
  headers_configured?: boolean;
}

interface IntegrationUpdate extends IntegrationDestination {
  url?: string;
  headers?: Record<string, string>;
}

interface IntegrationsResponse extends ApiBody {
  destinations?: IntegrationDestination[];
  destination?: IntegrationDestination;
  events?: string[];
}
