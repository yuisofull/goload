import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  ArrowLeft,
  Cloud,
  Copy,
  Download,
  Link2,
  Loader2,
  LogOut,
  Magnet,
  MoreHorizontal,
  Pause,
  Play,
  RefreshCw,
  Search,
  Trash2,
  Upload,
  X,
  XCircle,
  CheckCircle2,
  AlertCircle,
  FileText,
} from "lucide-react";
import { toast } from "sonner";
import { formatDistanceToNow } from "date-fns";

import { useAuth } from "@/contexts/AuthContext";
import {
  apiBaseUrl,
  cancelTask,
  createTask,
  deleteTask,
  extractError,
  generateDownloadUrl,
  getTaskProgress,
  listTasks,
  pauseTask,
  resumeTask,
  retryTask,
} from "@/lib/goload/client";
import type { Task } from "@/lib/goload/types";
import {
  fileNameFromUrl,
  fileToBase64,
  formatBytes,
  isBitTorrentSource,
  isMagnetUri,
  sourceTypeFromUrl,
  statusTone,
} from "@/lib/goload/utils";

const toneClasses: Record<ReturnType<typeof statusTone>, string> = {
  primary: "bg-primary-soft text-primary border-primary/20",
  success: "bg-success-soft text-success border-success/20",
  warning: "bg-warning-soft text-warning border-warning/20",
  destructive: "bg-destructive/10 text-destructive border-destructive/20",
  muted: "bg-secondary text-muted-foreground border-border",
};

const isActiveStatus = (s: string) =>
  /download|active|process|queue|pending|paus/i.test(s);

const Dashboard = () => {
  const { isAuthenticated, account, signOut } = useAuth();
  const navigate = useNavigate();

  const [tasks, setTasks] = useState<Task[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [search, setSearch] = useState("");

  const [url, setUrl] = useState("");
  const [creating, setCreating] = useState(false);
  const [openMenuId, setOpenMenuId] = useState<number | null>(null);
  const [torrentFile, setTorrentFile] = useState<File | null>(null);
  const torrentInputRef = useRef<HTMLInputElement | null>(null);

  const pollRef = useRef<number | null>(null);

  useEffect(() => {
    if (!isAuthenticated) {
      navigate("/", { replace: true });
    }
  }, [isAuthenticated, navigate]);

  const refresh = async (showSpinner = false) => {
    if (showSpinner) setLoading(true);
    try {
      const data = await listTasks(0, 100);
      setTasks(data.tasks ?? []);
      setLoadError(null);
    } catch (err) {
      setLoadError(extractError(err));
    } finally {
      if (showSpinner) setLoading(false);
    }
  };

  // Initial load + polling
  useEffect(() => {
    if (!isAuthenticated) return;
    refresh(true);
    pollRef.current = window.setInterval(() => refresh(false), 10000);
    return () => {
      if (pollRef.current) window.clearInterval(pollRef.current);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isAuthenticated]);

  // Per-active-task progress polling
  useEffect(() => {
    if (!isAuthenticated) return;
    const active = tasks.filter((t) => isActiveStatus(t.status));
    if (active.length === 0) return;

    const id = window.setInterval(async () => {
      const settled = await Promise.allSettled(
        active.map((t) => getTaskProgress(t.id).then((p) => ({ id: t.id, p })))
      );
      const updates = new Map<number, { progress: number; downloaded_bytes: number; total_bytes: number }>();
      for (const r of settled) {
        if (r.status === "fulfilled") updates.set(r.value.id, r.value.p);
      }
      setTasks((prev) =>
        prev.map((t) => {
          const p = updates.get(t.id);
          if (!p) return t;
          return {
            ...t,
            progress: p.progress,
            downloaded_bytes: p.downloaded_bytes,
            total_bytes: p.total_bytes,
          };
        })
      );
    }, 3000);
    return () => window.clearInterval(id);
  }, [isAuthenticated, tasks]);

  const handleCreate = async (e: React.FormEvent) => {
    e.preventDefault();
    const trimmed = url.trim();

    // BitTorrent: uploaded .torrent file mode (no URL needed).
    if (torrentFile && !trimmed) {
      setCreating(true);
      try {
        const base64 = await fileToBase64(torrentFile);
        const fileName = torrentFile.name.replace(/\.torrent$/i, "") || "torrent-download";
        const task = await createTask({
          file_name: fileName,
          source_url: "uploaded://torrent",
          source_type: "BITTORRENT",
          metadata: { torrent_file_base64: base64 },
        });
        toast.success("Torrent queued", { description: task.file_name });
        setTorrentFile(null);
        if (torrentInputRef.current) torrentInputRef.current.value = "";
        await refresh(false);
      } catch (err) {
        toast.error("Could not start torrent", { description: extractError(err) });
      } finally {
        setCreating(false);
      }
      return;
    }

    if (!trimmed) return;

    const isMagnet = isMagnetUri(trimmed);
    const isBT = isBitTorrentSource(trimmed);

    // Magnet links are not parsable by URL(), validate them separately.
    if (!isMagnet) {
      try {
        new URL(trimmed);
      } catch {
        toast.error("Please enter a valid URL or magnet link.");
        return;
      }
    }

    setCreating(true);
    try {
      const task = await createTask({
        file_name: fileNameFromUrl(trimmed),
        source_url: trimmed,
        source_type: sourceTypeFromUrl(trimmed),
      });
      toast.success(isBT ? "Torrent queued" : "Download queued", {
        description: task.file_name,
      });
      setUrl("");
      await refresh(false);
    } catch (err) {
      toast.error("Could not start download", { description: extractError(err) });
    } finally {
      setCreating(false);
    }
  };

  const onPickTorrent = (file: File | null) => {
    if (!file) {
      setTorrentFile(null);
      return;
    }
    if (!/\.torrent$/i.test(file.name)) {
      toast.error("Please select a .torrent file.");
      if (torrentInputRef.current) torrentInputRef.current.value = "";
      return;
    }
    setTorrentFile(file);
    setUrl("");
  };

  const withAction = async (
    label: string,
    fn: () => Promise<unknown>,
    successMsg?: string
  ) => {
    setOpenMenuId(null);
    try {
      await fn();
      if (successMsg) toast.success(successMsg);
      await refresh(false);
    } catch (err) {
      toast.error(`${label} failed`, { description: extractError(err) });
    }
  };

  const handleGetLink = async (task: Task) => {
    setOpenMenuId(null);
    try {
      const res = await generateDownloadUrl({
        task_id: task.id,
        ttl_seconds: 3600,
        one_time: false,
      });
      const isAbsoluteUrl = /^[a-zA-Z][a-zA-Z\d+\-.]*:/.test(res.url);

      const fullUrl = isAbsoluteUrl
        ? res.url
        : `${apiBaseUrl.replace(/\/$/, "")}/${res.url.replace(/^\/+/, "")}`;
      await navigator.clipboard.writeText(fullUrl);

      const isFile = fullUrl.startsWith("file://");

      if (isFile) {
        // This will usually fail in browsers
        window.open(fullUrl);
      } else {
        window.open(fullUrl);
      }
      toast.success("Download link copied", {
        description: "Link is valid for 1 hour.",
      });
    } catch (err) {
      toast.error("Could not copy link", { description: extractError(err) });
    }
  };

  const handleDownload = async (task: Task) => {
    setOpenMenuId(null);
    try {
      const res = await generateDownloadUrl({
        task_id: task.id,
        ttl_seconds: 3600,
        one_time: false,
      });
      const fullUrl = res.url.startsWith("http")
        ? res.url
        : `${apiBaseUrl.replace(/\/$/, "")}${res.url}`;
      // Try to initiate a download; fallback to opening in a new tab.
      const a = document.createElement("a");
      a.href = fullUrl;
      a.download = task.file_name ?? "";
      a.rel = "noopener";
      document.body.appendChild(a);
      a.click();
      a.remove();
      toast.success("Download started", { description: task.file_name });
    } catch (err) {
      toast.error("Could not start download", { description: extractError(err) });
    }
  };

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return tasks;
    return tasks.filter(
      (t) =>
        t.file_name.toLowerCase().includes(q) ||
        t.source_url.toLowerCase().includes(q)
    );
  }, [tasks, search]);

  const stats = useMemo(() => {
    const total = tasks.length;
    const active = tasks.filter((t) => isActiveStatus(t.status)).length;
    const completed = tasks.filter((t) =>
      /complete|done|ready/i.test(t.status)
    ).length;
    const totalBytes = tasks.reduce(
      (sum, t) => sum + (t.total_bytes ?? 0),
      0
    );
    return { total, active, completed, totalBytes };
  }, [tasks]);

  if (!isAuthenticated) return null;

  return (
    <div className="min-h-screen bg-background">
      {/* Top bar */}
      <header className="sticky top-0 z-40 bg-background/85 backdrop-blur-xl border-b border-border/40">
        <div className="container max-w-[1200px] flex items-center justify-between py-4">
          <div className="flex items-center gap-6">
            <Link to="/" className="flex items-center gap-2 group">
              <span className="grid place-items-center size-8 rounded-xl bg-gradient-primary shadow-glow ease-soft transition-transform group-hover:scale-105">
                <Cloud className="size-4 text-primary-foreground" strokeWidth={2.5} />
              </span>
              <span className="font-display text-lg font-bold tracking-tight">Goload</span>
            </Link>
            <Link
              to="/"
              className="hidden sm:inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground transition-colors"
            >
              <ArrowLeft className="size-3.5" /> Back to site
            </Link>
          </div>
          <div className="flex items-center gap-3">
            <span className="hidden md:inline text-xs text-muted-foreground tabular-nums">
              {apiBaseUrl}
            </span>
            <div className="flex items-center gap-2 bg-secondary rounded-full pl-1 pr-3 py-1">
              <span className="grid place-items-center size-7 rounded-full bg-gradient-primary text-primary-foreground text-xs font-bold">
                {account?.account_name?.[0]?.toUpperCase() ?? "U"}
              </span>
              <span className="text-sm font-medium hidden sm:inline">
                {account?.account_name}
              </span>
            </div>
            <button
              onClick={() => { signOut(); navigate("/"); }}
              className="size-9 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
              aria-label="Sign out"
              title="Sign out"
            >
              <LogOut className="size-4" />
            </button>
          </div>
        </div>
      </header>

      <main className="container max-w-[1200px] py-10 space-y-8">
        {/* Welcome */}
        <div>
          <h1 className="font-display text-3xl md:text-4xl font-bold tracking-tight">
            Welcome back, {account?.account_name}.
          </h1>
          <p className="text-muted-foreground mt-1">
            Your data cloud is running at peak performance.
          </p>
        </div>

        {/* Create task */}
        {(() => {
          const trimmed = url.trim();
          const detectedBT = isBitTorrentSource(trimmed);
          const hasInput = !!trimmed || !!torrentFile;
          return (
            <form
              onSubmit={handleCreate}
              className="relative flex flex-col gap-2 p-2 bg-card rounded-[24px] shadow-soft border border-border/40"
            >
              <div className="flex items-center gap-2">
                <div
                  className={`grid place-items-center size-12 rounded-xl shrink-0 transition-colors ${detectedBT || torrentFile
                    ? "bg-primary-soft text-primary"
                    : "bg-secondary text-primary"
                    }`}
                >
                  {detectedBT || torrentFile ? (
                    <Magnet className="size-5" />
                  ) : (
                    <Link2 className="size-5" />
                  )}
                </div>
                <input
                  type="text"
                  value={url}
                  onChange={(e) => {
                    setUrl(e.target.value);
                    if (e.target.value && torrentFile) {
                      setTorrentFile(null);
                      if (torrentInputRef.current) torrentInputRef.current.value = "";
                    }
                  }}
                  placeholder={
                    torrentFile
                      ? "Using uploaded .torrent file"
                      : "Paste a URL, magnet link, or .torrent URL…"
                  }
                  disabled={creating || !!torrentFile}
                  className="flex-1 bg-transparent border-0 outline-none px-2 h-14 text-base placeholder:text-muted-foreground/60 min-w-0 disabled:cursor-not-allowed"
                />

                <input
                  ref={torrentInputRef}
                  type="file"
                  accept=".torrent,application/x-bittorrent"
                  className="hidden"
                  onChange={(e) => onPickTorrent(e.target.files?.[0] ?? null)}
                />
                <button
                  type="button"
                  onClick={() => torrentInputRef.current?.click()}
                  disabled={creating}
                  title="Upload .torrent file"
                  className="hidden sm:inline-flex items-center gap-1.5 h-14 px-4 rounded-2xl bg-secondary text-foreground hover:bg-muted transition-colors text-sm font-medium disabled:opacity-60"
                >
                  <Upload className="size-4" />
                  <span className="hidden md:inline">.torrent</span>
                </button>

                <button
                  type="submit"
                  disabled={creating || !hasInput}
                  className="inline-flex items-center gap-2 h-14 px-6 rounded-2xl bg-gradient-primary text-primary-foreground font-medium shadow-glow hover:shadow-elevated active:scale-[0.98] transition-all ease-soft whitespace-nowrap disabled:opacity-60 disabled:cursor-not-allowed"
                >
                  {creating ? <Loader2 className="size-5 animate-spin" /> : "Goload Now"}
                </button>
              </div>

              {(detectedBT || torrentFile) && (
                <div className="flex items-center justify-between gap-3 px-3 py-2 mx-1 rounded-xl bg-primary-soft/60 text-primary-soft-foreground text-xs">
                  <div className="flex items-center gap-2 min-w-0">
                    <Magnet className="size-3.5 shrink-0" />
                    <span className="font-medium truncate">
                      {torrentFile
                        ? `Torrent file: ${torrentFile.name}`
                        : isMagnetUri(trimmed)
                          ? "Magnet link detected — will be downloaded via BitTorrent"
                          : ".torrent URL detected — will be downloaded via BitTorrent"}
                    </span>
                  </div>
                  {torrentFile && (
                    <button
                      type="button"
                      onClick={() => onPickTorrent(null)}
                      className="size-6 grid place-items-center rounded-full hover:bg-primary/10 shrink-0"
                      aria-label="Remove torrent file"
                    >
                      <X className="size-3.5" />
                    </button>
                  )}
                </div>
              )}

              {/* Mobile-only torrent upload button */}
              <button
                type="button"
                onClick={() => torrentInputRef.current?.click()}
                disabled={creating}
                className="sm:hidden inline-flex items-center justify-center gap-2 h-11 mx-1 rounded-xl bg-secondary text-foreground hover:bg-muted transition-colors text-sm font-medium disabled:opacity-60"
              >
                <Upload className="size-4" />
                Upload .torrent file
              </button>
            </form>
          );
        })()}

        {/* Stats */}
        <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
          <StatCard label="Total Tasks" value={stats.total.toString()} icon={<FileText className="size-4" />} tone="primary" />
          <StatCard label="Active" value={stats.active.toString()} icon={<Loader2 className="size-4" />} tone="primary" />
          <StatCard label="Completed" value={stats.completed.toString()} icon={<CheckCircle2 className="size-4" />} tone="success" />
          <StatCard label="Storage Used" value={formatBytes(stats.totalBytes)} icon={<Cloud className="size-4" />} tone="muted" />
        </div>

        {/* Tasks list */}
        <div className="bg-card rounded-[24px] shadow-soft border border-border/40 overflow-hidden">
          <div className="flex flex-col sm:flex-row sm:items-center justify-between gap-3 px-6 py-5 border-b border-border/40">
            <div className="flex items-center gap-3">
              <h2 className="font-display font-semibold text-base">Downloads</h2>
              <span className="text-xs text-muted-foreground bg-secondary rounded-full px-2.5 py-0.5">
                {filtered.length}
              </span>
            </div>
            <div className="flex items-center gap-2">
              <div className="relative">
                <Search className="size-4 absolute left-3 top-1/2 -translate-y-1/2 text-muted-foreground" />
                <input
                  value={search}
                  onChange={(e) => setSearch(e.target.value)}
                  placeholder="Search…"
                  className="h-9 pl-9 pr-3 w-48 rounded-full bg-secondary/60 border border-transparent text-sm outline-none focus:border-primary focus:bg-card transition-all"
                />
              </div>
              <button
                onClick={() => refresh(true)}
                className="size-9 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
                title="Refresh"
              >
                <RefreshCw className={`size-4 ${loading ? "animate-spin" : ""}`} />
              </button>
            </div>
          </div>

          {loading ? (
            <div className="p-12 grid place-items-center text-muted-foreground">
              <Loader2 className="size-6 animate-spin" />
            </div>
          ) : loadError ? (
            <div className="p-10 text-center">
              <AlertCircle className="size-8 text-destructive mx-auto mb-3" />
              <p className="font-medium text-destructive mb-1">Could not load tasks</p>
              <p className="text-sm text-muted-foreground max-w-md mx-auto">{loadError}</p>
              <button
                onClick={() => refresh(true)}
                className="mt-4 inline-flex items-center gap-2 h-10 px-4 rounded-full bg-secondary hover:bg-muted transition-colors text-sm"
              >
                <RefreshCw className="size-3.5" /> Try again
              </button>
            </div>
          ) : filtered.length === 0 ? (
            <div className="p-16 text-center text-muted-foreground">
              <Download className="size-8 mx-auto mb-3 opacity-50" />
              <p className="font-medium mb-1 text-foreground">
                {tasks.length === 0 ? "No downloads yet" : "No matches"}
              </p>
              <p className="text-sm">
                {tasks.length === 0
                  ? "Paste a URL above to start your first download."
                  : "Try a different search term."}
              </p>
            </div>
          ) : (
            <ul className="divide-y divide-border/40">
              {filtered.map((task) => (
                <TaskRow
                  key={task.id}
                  task={task}
                  menuOpen={openMenuId === task.id}
                  onToggleMenu={() =>
                    setOpenMenuId((id) => (id === task.id ? null : task.id))
                  }
                  onCloseMenu={() => setOpenMenuId(null)}
                  onPause={() => withAction("Pause", () => pauseTask(task.id), "Paused")}
                  onResume={() => withAction("Resume", () => resumeTask(task.id), "Resumed")}
                  onCancel={() => withAction("Cancel", () => cancelTask(task.id), "Cancelled")}
                  onRetry={() => withAction("Retry", () => retryTask(task.id), "Retrying")}
                  onDelete={() => withAction("Delete", () => deleteTask(task.id), "Deleted")}
                  onGetLink={() => handleGetLink(task)}
                  onDownload={() => handleDownload(task)}
                />
              ))}
            </ul>
          )}
        </div>
      </main>
    </div>
  );
};

// ----- Subcomponents --------------------------------------------------

const StatCard = ({
  label,
  value,
  icon,
  tone,
}: {
  label: string;
  value: string;
  icon: React.ReactNode;
  tone: "primary" | "success" | "muted";
}) => {
  const toneBg: Record<typeof tone, string> = {
    primary: "bg-primary-soft text-primary",
    success: "bg-success-soft text-success",
    muted: "bg-secondary text-muted-foreground",
  };
  return (
    <div className="bg-card rounded-2xl border border-border/40 p-5 flex items-start justify-between shadow-soft">
      <div>
        <p className="text-[10px] font-semibold uppercase tracking-wider text-muted-foreground mb-2">
          {label}
        </p>
        <p className="font-display text-2xl font-bold tabular-nums">{value}</p>
      </div>
      <span className={`grid place-items-center size-9 rounded-xl ${toneBg[tone]}`}>
        {icon}
      </span>
    </div>
  );
};

interface TaskRowProps {
  task: Task;
  menuOpen: boolean;
  onToggleMenu: () => void;
  onCloseMenu: () => void;
  onPause: () => void;
  onResume: () => void;
  onCancel: () => void;
  onRetry: () => void;
  onDelete: () => void;
  onGetLink: () => void;
  onDownload: () => void;
}

const TaskRow = ({
  task,
  menuOpen,
  onToggleMenu,
  onCloseMenu,
  onPause,
  onResume,
  onCancel,
  onRetry,
  onDelete,
  onGetLink,
  onDownload,
}: TaskRowProps) => {
  const tone = statusTone(task.status);
  const isActive = isActiveStatus(task.status);
  const isPaused = /paus/i.test(task.status);
  const isCompleted = /complete|ready|done/i.test(task.status);
  const isFailed = /fail|error/i.test(task.status);
  const isCancelled = /cancel/i.test(task.status);
  const bytesProgress =
    task.total_bytes && task.total_bytes > 0
      ? Math.max(
        0,
        Math.min(
          100,
          Math.round(((task.downloaded_bytes ?? 0) / task.total_bytes) * 100)
        )
      )
      : null;

  const displayProgress =
    bytesProgress ??
    (task.progress == null
      ? (isCompleted ? 100 : 0)
      : task.progress <= 1
        ? Math.round(task.progress * 100)
        : Math.round(task.progress));

  const isTorrent = /bittorrent|torrent/i.test(task.source_type);

  return (
    <li className="px-6 py-5 hover:bg-secondary/30 transition-colors group">
      <div className="flex items-center gap-4">
        <div className={`grid place-items-center size-12 rounded-xl shrink-0 ${isCompleted ? "bg-success-soft text-success"
          : isTorrent ? "bg-primary-soft text-primary"
            : "bg-secondary text-muted-foreground"
          }`}>
          {isCompleted
            ? <CheckCircle2 className="size-5" />
            : isTorrent ? <Magnet className="size-5" />
              : <FileText className="size-5" />}
        </div>

        <div className="flex-1 min-w-0">
          <div className="flex items-center justify-between gap-3 mb-1.5">
            <p className="font-medium text-sm truncate">{task.file_name}</p>
            <span
              className={`text-[10px] font-semibold uppercase tracking-wider px-2.5 py-1 rounded-full border whitespace-nowrap ${toneClasses[tone]}`}
            >
              {task.status}
            </span>
          </div>

          <div className="flex items-center gap-3">
            <div className="flex-1 h-2 rounded-full bg-secondary overflow-hidden">
              <div
                className={`h-full rounded-full transition-all duration-500 ${isCompleted ? "bg-success"
                  : isFailed || isCancelled ? "bg-destructive"
                    : "bg-gradient-primary"
                  }`}
                style={{ width: `${displayProgress}%` }}
              />
            </div>
            <span className="text-xs font-medium text-muted-foreground tabular-nums w-32 text-right shrink-0">
              {displayProgress}% · {formatBytes(task.downloaded_bytes)} / {formatBytes(task.total_bytes)}
            </span>
          </div>

          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 mt-2 text-xs text-muted-foreground">
            <span className="truncate max-w-md" title={task.source_url}>
              {task.source_url}
            </span>
            {task.created_at && (
              <span>
                · {formatDistanceToNow(new Date(task.created_at), { addSuffix: true })}
              </span>
            )}
            {task.error_message && (
              <span className="text-destructive">· {task.error_message}</span>
            )}
          </div>
        </div>

        {/* Quick actions */}
        <div className="flex items-center gap-1 shrink-0">
          {isCompleted && (
            <button
              onClick={onDownload}
              className="inline-flex items-center gap-1.5 h-9 px-3 rounded-full bg-gradient-primary text-primary-foreground text-xs font-medium shadow-glow hover:shadow-elevated transition-all"
            >
              <Download className="size-3.5" />
              <span className="hidden sm:inline">Download</span>
            </button>
          )}
          {isActive && !isPaused && (
            <button
              onClick={onPause}
              className="size-9 grid place-items-center rounded-full bg-secondary text-foreground hover:bg-muted transition-colors"
              title="Pause"
            >
              <Pause className="size-3.5" />
            </button>
          )}
          {isPaused && (
            <button
              onClick={onResume}
              className="size-9 grid place-items-center rounded-full bg-secondary text-foreground hover:bg-muted transition-colors"
              title="Resume"
            >
              <Play className="size-3.5" />
            </button>
          )}
          {(isFailed || isCancelled) && (
            <button
              onClick={onRetry}
              className="size-9 grid place-items-center rounded-full bg-secondary text-foreground hover:bg-muted transition-colors"
              title="Retry"
            >
              <RefreshCw className="size-3.5" />
            </button>
          )}

          <div className="relative">
            <button
              onClick={onToggleMenu}
              className="size-9 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
            >
              <MoreHorizontal className="size-4" />
            </button>
            {menuOpen && (
              <>
                <div className="fixed inset-0 z-40" onClick={onCloseMenu} />
                <div className="absolute right-0 top-10 z-50 w-48 bg-card rounded-2xl shadow-elevated border border-border/40 p-1.5 animate-in fade-in zoom-in-95 duration-150">
                  {isCompleted && (
                    <MenuItem icon={<Copy className="size-3.5" />} label="Copy link" onClick={onGetLink} />
                  )}
                  {isActive && !isPaused && (
                    <MenuItem icon={<Pause className="size-3.5" />} label="Pause" onClick={onPause} />
                  )}
                  {isPaused && (
                    <MenuItem icon={<Play className="size-3.5" />} label="Resume" onClick={onResume} />
                  )}
                  {(isActive || isPaused) && (
                    <MenuItem icon={<XCircle className="size-3.5" />} label="Cancel" onClick={onCancel} />
                  )}
                  {(isFailed || isCancelled) && (
                    <MenuItem icon={<RefreshCw className="size-3.5" />} label="Retry" onClick={onRetry} />
                  )}
                  <div className="h-px bg-border/60 my-1" />
                  <MenuItem
                    icon={<Trash2 className="size-3.5" />}
                    label="Delete"
                    onClick={onDelete}
                    destructive
                  />
                </div>
              </>
            )}
          </div>
        </div>
      </div>
    </li>
  );
};

const MenuItem = ({
  icon,
  label,
  onClick,
  destructive,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  destructive?: boolean;
}) => (
  <button
    onClick={onClick}
    className={`w-full flex items-center gap-2.5 px-3 py-2 rounded-xl text-sm transition-colors ${destructive
      ? "text-destructive hover:bg-destructive/10"
      : "text-foreground hover:bg-secondary"
      }`}
  >
    <span className="text-muted-foreground">{icon}</span>
    {label}
  </button>
);

export default Dashboard;
