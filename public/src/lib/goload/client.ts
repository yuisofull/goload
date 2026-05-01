import axios, { AxiosError, AxiosInstance } from "axios";
import type {
  CreateAccountResponse,
  CreateSessionResponse,
  CreateTaskRequest,
  GenerateDownloadURLRequest,
  GenerateDownloadURLResponse,
  GetTaskProgressResponse,
  ListTasksResponse,
  Task,
} from "./types";

const TOKEN_KEY = "goload.token";
const ACCOUNT_KEY = "goload.account";

export const getStoredToken = () => localStorage.getItem(TOKEN_KEY);
export const setStoredToken = (token: string | null) => {
  if (token) localStorage.setItem(TOKEN_KEY, token);
  else localStorage.removeItem(TOKEN_KEY);
};
export const getStoredAccount = () => {
  const raw = localStorage.getItem(ACCOUNT_KEY);
  return raw ? JSON.parse(raw) : null;
};
export const setStoredAccount = (account: unknown) => {
  if (account) localStorage.setItem(ACCOUNT_KEY, JSON.stringify(account));
  else localStorage.removeItem(ACCOUNT_KEY);
};

const baseURL =
  (import.meta.env.VITE_GOLOAD_API_URL as string | undefined) ?? "";

export const isPocketMode =
  (import.meta.env.VITE_GOLOAD_POCKET as string | undefined) === "true";

export const api: AxiosInstance = axios.create({
  baseURL,
  // Keep the timeout generous; download tasks may be slow to register.
  timeout: 30_000,
});

api.interceptors.request.use((config) => {
  const token = getStoredToken();
  if (token && config.headers) {
    config.headers.set("Authorization", `Bearer ${token}`);
  }
  return config;
});

let onUnauthorized: (() => void) | null = null;
export const setUnauthorizedHandler = (cb: (() => void) | null) => {
  onUnauthorized = cb;
};

api.interceptors.response.use(
  (r) => r,
  (error: AxiosError) => {
    if (error.response?.status === 401 && onUnauthorized) onUnauthorized();
    return Promise.reject(error);
  }
);

// Helpers ---------------------------------------------------------------

export const extractError = (err: unknown): string => {
  if (axios.isAxiosError(err)) {
    if (err.code === "ERR_NETWORK") {
      return `Cannot reach Goload API at ${baseURL}. Is the server running and CORS enabled?`;
    }
    const data = err.response?.data as { error?: string } | undefined;
    if (data?.error) return data.error;
    return err.message;
  }
  return err instanceof Error ? err.message : "Unknown error";
};

// Auth ------------------------------------------------------------------

export const createAccount = (account_name: string, password: string) =>
  api
    .post<CreateAccountResponse>("/api/v1/auth/create", { account_name, password })
    .then((r) => r.data);

export const createSession = (account_name: string, password: string) =>
  api
    .post<CreateSessionResponse>("/api/v1/auth/session", { account_name, password })
    .then((r) => r.data);

// Tasks -----------------------------------------------------------------

export const listTasks = (offset = 0, limit = 50) =>
  api
    .get<ListTasksResponse>("/api/v1/tasks/list", { params: { offset, limit } })
    .then((r) => r.data);

export const createTask = (body: CreateTaskRequest) =>
  api
    .post<{ task: Task }>("/api/v1/tasks/create", body)
    .then((r) => r.data.task);

export const getTask = (id: number) =>
  api
    .get<{ task: Task }>("/api/v1/tasks/get", { params: { id } })
    .then((r) => r.data.task);

export const deleteTask = (id: number) =>
  api.delete("/api/v1/tasks/delete", { params: { id } }).then((r) => r.data);

export const pauseTask = (id: number) =>
  api.post("/api/v1/tasks/pause", null, { params: { id } }).then((r) => r.data);

export const resumeTask = (id: number) =>
  api.post("/api/v1/tasks/resume", null, { params: { id } }).then((r) => r.data);

export const cancelTask = (id: number) =>
  api.post("/api/v1/tasks/cancel", null, { params: { id } }).then((r) => r.data);

export const retryTask = (id: number) =>
  api.post("/api/v1/tasks/retry", null, { params: { id } }).then((r) => r.data);

export const getTaskProgress = (task_id: number) =>
  api
    .get<GetTaskProgressResponse>("/api/v1/tasks/progress", { params: { task_id } })
    .then((r) => r.data);

export const generateDownloadUrl = (body: GenerateDownloadURLRequest) =>
  api
    .post<GenerateDownloadURLResponse>("/api/v1/tasks/download-url", body)
    .then((r) => r.data);

export const revealTaskInFolder = (id: number) =>
  api
    .post<{ path: string }>("/api/v1/pocket/tasks/reveal", null, { params: { id } })
    .then((r) => r.data);

export const apiBaseUrl = baseURL;

export const toAbsoluteApiUrl = (url: string): string => {
  if (/^[a-zA-Z][a-zA-Z\d+\-.]*:/.test(url)) return url;

  const base =
    baseURL && /^[a-zA-Z][a-zA-Z\d+\-.]*:/.test(baseURL)
      ? baseURL
      : window.location.origin;

  return `${base.replace(/\/$/, "")}/${url.replace(/^\/+/, "")}`;
};
