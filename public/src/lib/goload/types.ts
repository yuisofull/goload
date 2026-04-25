// Types mirror the Goload OpenAPI spec.

export type TaskStatus =
  | "pending"
  | "queued"
  | "downloading"
  | "paused"
  | "completed"
  | "failed"
  | "cancelled"
  | string; // tolerate unknown server values

export interface Task {
  id: number;
  of_account_id: number;
  file_name: string;
  source_url: string;
  source_type: string;
  checksum_type?: string | null;
  checksum_value?: string | null;
  status: TaskStatus;
  progress?: number | null;
  downloaded_bytes?: number | null;
  total_bytes?: number | null;
  error_message?: string | null;
  metadata?: Record<string, unknown> | null;
  created_at?: string | null;
  updated_at?: string | null;
  completed_at?: string | null;
}

export interface AuthAccount {
  id: number;
  account_name: string;
}

export interface ListTasksResponse {
  tasks: Task[];
  total_count: number;
}

export interface CreateTaskRequest {
  file_name: string;
  source_url: string;
  source_type: string;
  checksum_type?: string;
  checksum_value?: string;
  metadata?: Record<string, unknown>;
}

export interface CreateSessionResponse {
  token: string;
  account: AuthAccount;
}

export interface CreateAccountResponse {
  id: number;
  account_name: string;
}

export interface GetTaskProgressResponse {
  progress: number;
  downloaded_bytes: number;
  total_bytes: number;
}

export interface GenerateDownloadURLRequest {
  task_id: number;
  ttl_seconds: number;
  one_time: boolean;
}

export interface GenerateDownloadURLResponse {
  url: string;
  direct: boolean;
}
