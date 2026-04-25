import { FileText, FileArchive, Film, MoreHorizontal, Pause, Download } from "lucide-react";

type Item = {
  icon: typeof FileText;
  name: string;
  meta: string;
  status: "downloading" | "storing" | "ready";
  progress: number;
  speed?: string;
};

const items: Item[] = [
  {
    icon: FileText,
    name: "Annual_Report_2024.pdf",
    meta: "12.4 MB",
    status: "downloading",
    progress: 65,
    speed: "1.2 GB/s",
  },
  {
    icon: FileArchive,
    name: "Raw_Assets_Archive.zip",
    meta: "4.8 GB · Encrypting…",
    status: "storing",
    progress: 98,
  },
  {
    icon: Film,
    name: "Product_Promo_4K.mp4",
    meta: "Completed 2 mins ago",
    status: "ready",
    progress: 100,
  },
];

const statusStyles: Record<Item["status"], string> = {
  downloading: "bg-primary-soft text-primary border-primary/20",
  storing: "bg-warning-soft text-warning border-warning/20",
  ready: "bg-success-soft text-success border-success/20",
};

const statusLabels: Record<Item["status"], string> = {
  downloading: "Downloading",
  storing: "Storing",
  ready: "Ready",
};

const DashboardPreview = () => {
  return (
    <div className="bg-card rounded-[28px] shadow-elevated border border-border/40 overflow-hidden">
      {/* Window chrome */}
      <div className="flex items-center justify-between px-6 py-5 border-b border-border/40 bg-surface-low/40">
        <div className="flex items-center gap-3">
          <div className="flex gap-1.5">
            <span className="size-2.5 rounded-full bg-destructive/40" />
            <span className="size-2.5 rounded-full bg-warning/40" />
            <span className="size-2.5 rounded-full bg-success/40" />
          </div>
          <h3 className="font-display font-semibold text-base ml-3">
            Downloads in progress
          </h3>
        </div>
        <button className="size-8 grid place-items-center rounded-lg text-muted-foreground hover:bg-secondary transition-colors">
          <MoreHorizontal className="size-4" />
        </button>
      </div>

      <div className="p-4 md:p-6 space-y-4">
        {items.map((item) => {
          const Icon = item.icon;
          const isDownloading = item.status === "downloading";
          const isReady = item.status === "ready";
          return (
            <div
              key={item.name}
              className="flex flex-col md:flex-row md:items-center gap-4 p-3 rounded-2xl hover:bg-secondary/40 transition-colors"
            >
              <div className="flex items-center gap-4 md:flex-1 min-w-0">
                <div className="grid place-items-center size-12 rounded-xl bg-secondary text-muted-foreground shrink-0">
                  <Icon className="size-5" />
                </div>
                <div className="flex-1 min-w-0">
                  <div className="flex items-center justify-between gap-3 mb-2">
                    <p className="font-medium text-sm truncate">{item.name}</p>
                    <span
                      className={`text-[10px] font-semibold uppercase tracking-wider px-2.5 py-1 rounded-full border whitespace-nowrap ${statusStyles[item.status]}`}
                    >
                      {statusLabels[item.status]}
                    </span>
                  </div>
                  <div className="flex items-center gap-3">
                    <div className="flex-1 h-2 rounded-full bg-secondary overflow-hidden">
                      <div
                        className={`h-full rounded-full transition-all duration-500 ${
                          isReady
                            ? "bg-success"
                            : item.status === "storing"
                            ? "bg-accent"
                            : "bg-gradient-primary"
                        }`}
                        style={{ width: `${item.progress}%` }}
                      />
                    </div>
                    <span className="text-xs font-medium text-muted-foreground tabular-nums w-24 text-right">
                      {isDownloading && item.speed
                        ? `${item.progress}% · ${item.speed}`
                        : `${item.progress}% · ${item.meta.split("·")[0].trim()}`}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex items-center gap-2 md:pl-2">
                {isReady ? (
                  <button className="inline-flex items-center gap-1.5 h-9 px-4 rounded-full bg-gradient-primary text-primary-foreground text-xs font-medium shadow-glow hover:shadow-elevated transition-shadow">
                    <Download className="size-3.5" />
                    Download
                  </button>
                ) : (
                  <button className="size-9 grid place-items-center rounded-full bg-secondary text-muted-foreground hover:bg-muted transition-colors">
                    <Pause className="size-3.5" />
                  </button>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
};

export default DashboardPreview;
