import { useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import { ArrowRight, Link2, Loader2, Magnet, Paperclip, Sparkles, X } from "lucide-react";
import DashboardPreview from "./DashboardPreview";
import { useAuth } from "@/contexts/AuthContext";
import { useAuthModal } from "@/contexts/AuthModalContext";
import { createTask, extractError } from "@/lib/goload/client";
import {
  fileNameFromUrl,
  fileToBase64,
  isBitTorrentSource,
  isMagnetUri,
  sourceTypeFromUrl,
} from "@/lib/goload/utils";
import { toast } from "sonner";

const Hero = () => {
  const { isAuthenticated } = useAuth();
  const { openModal } = useAuthModal();
  const navigate = useNavigate();
  const [url, setUrl] = useState("");
  const [torrentFile, setTorrentFile] = useState<File | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const fileInputRef = useRef<HTMLInputElement | null>(null);

  const isTorrentish = !!torrentFile || isBitTorrentSource(url);

  const validateUrl = (raw: string) => {
    if (isMagnetUri(raw)) return true;
    try {
      const u = new URL(raw);
      return /^https?:$/.test(u.protocol);
    } catch {
      return false;
    }
  };

  const handlePickFile = () => fileInputRef.current?.click();

  const handleFileChange = (e: React.ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    if (!/\.torrent$/i.test(file.name)) {
      toast.error("Please choose a .torrent file.");
      return;
    }
    setTorrentFile(file);
    setUrl("");
  };

  const clearTorrentFile = () => setTorrentFile(null);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    // Mode 1: uploaded .torrent file
    if (torrentFile) {
      if (!isAuthenticated) {
        openModal({ mode: "signin" });
        toast.message("Sign in to start your torrent download.");
        return;
      }
      setSubmitting(true);
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
        navigate("/dashboard");
      } catch (err) {
        toast.error("Could not start download", { description: extractError(err) });
      } finally {
        setSubmitting(false);
      }
      return;
    }

    // Mode 2: URL / magnet
    const trimmed = url.trim();
    if (!trimmed) {
      toast.error("Paste a URL, magnet link, or upload a .torrent file.");
      return;
    }
    if (!validateUrl(trimmed)) {
      toast.error("Enter a valid http(s):// URL or magnet: link.");
      return;
    }

    if (!isAuthenticated) {
      openModal({ mode: "signin", pendingUrl: trimmed });
      return;
    }

    setSubmitting(true);
    try {
      const task = await createTask({
        file_name: fileNameFromUrl(trimmed),
        source_url: trimmed,
        source_type: sourceTypeFromUrl(trimmed),
      });
      toast.success("Download queued", { description: task.file_name });
      setUrl("");
      navigate("/dashboard");
    } catch (err) {
      toast.error("Could not start download", { description: extractError(err) });
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <section className="relative pt-40 pb-24 overflow-hidden">
      <div className="absolute inset-0 bg-gradient-hero pointer-events-none" />
      <div className="absolute top-20 left-1/2 -translate-x-1/2 w-[800px] h-[800px] rounded-full bg-primary/5 blur-3xl pointer-events-none" />

      <div className="container max-w-[1200px] relative">
        <div className="text-center max-w-3xl mx-auto animate-fade-up">
          <div className="inline-flex items-center gap-2 px-3 py-1.5 rounded-full bg-card border border-border/60 shadow-soft mb-8">
            <Sparkles className="size-3.5 text-primary" />
            <span className="text-xs font-medium tracking-tight text-muted-foreground">
              Now with BitTorrent — magnets &amp; .torrent files
            </span>
          </div>

          <h1 className="font-display text-5xl md:text-7xl lg:text-[80px] font-bold tracking-tightest leading-[1.02] text-balance mb-6">
            Your files,{" "}
            <span className="bg-gradient-to-r from-foreground via-primary to-accent bg-clip-text text-transparent">
              anywhere.
            </span>
          </h1>

          <p className="text-lg md:text-xl text-muted-foreground max-w-2xl mx-auto mb-12 text-balance leading-relaxed">
            Paste a link, drop a magnet, or upload a .torrent. Goload pulls it
            down at line speed and parks it in your private cloud vault.
          </p>

          <div
            className="relative max-w-3xl mx-auto group animate-fade-up"
            style={{ animationDelay: "120ms" }}
          >
            <div className="absolute -inset-2 bg-gradient-primary rounded-[28px] blur-xl opacity-15 group-hover:opacity-25 transition-opacity duration-700" />
            <form
              onSubmit={handleSubmit}
              className="relative flex items-center gap-2 p-2 bg-card rounded-[24px] shadow-elevated border border-border/40"
            >
              <div className="grid place-items-center size-12 rounded-xl bg-secondary text-muted-foreground shrink-0">
                {isTorrentish ? <Magnet className="size-5 text-primary" /> : <Link2 className="size-5" />}
              </div>

              {torrentFile ? (
                <div className="flex-1 flex items-center gap-2 min-w-0 px-2">
                  <span className="inline-flex items-center gap-1.5 px-2.5 py-1 rounded-full bg-primary-soft text-primary text-xs font-medium border border-primary/20 max-w-full">
                    <Magnet className="size-3.5 shrink-0" />
                    <span className="truncate">{torrentFile.name}</span>
                    <button
                      type="button"
                      onClick={clearTorrentFile}
                      className="ml-0.5 hover:bg-primary/10 rounded-full p-0.5 transition-colors"
                      aria-label="Remove torrent file"
                    >
                      <X className="size-3" />
                    </button>
                  </span>
                </div>
              ) : (
                <input
                  type="text"
                  value={url}
                  onChange={(e) => setUrl(e.target.value)}
                  placeholder="https://… , magnet:?xt=urn:btih:… , or upload .torrent →"
                  className="flex-1 bg-transparent border-0 outline-none px-2 h-14 text-base placeholder:text-muted-foreground/60 min-w-0"
                  disabled={submitting}
                />
              )}

              <label
                title="Upload .torrent file"
                aria-label="Upload .torrent file"
                className={`grid place-items-center size-12 rounded-xl bg-secondary text-muted-foreground hover:bg-muted hover:text-foreground transition-colors shrink-0 cursor-pointer ${submitting ? "pointer-events-none opacity-60" : ""}`}
              >
                <Paperclip className="size-5" />
                <input
                  ref={fileInputRef}
                  type="file"
                  accept=".torrent,application/x-bittorrent"
                  onChange={handleFileChange}
                  disabled={submitting}
                  className="sr-only"
                />
              </label>

              <button
                type="submit"
                disabled={submitting}
                className="inline-flex items-center gap-2 h-14 px-6 md:px-8 rounded-2xl bg-gradient-primary text-primary-foreground font-medium shadow-glow hover:shadow-elevated active:scale-[0.98] transition-all ease-soft whitespace-nowrap disabled:opacity-70"
              >
                {submitting ? (
                  <Loader2 className="size-5 animate-spin" />
                ) : (
                  <>
                    <span className="hidden sm:inline">Start Download</span>
                    <ArrowRight className="size-5" />
                  </>
                )}
              </button>
            </form>
          </div>

          <div className="flex flex-wrap justify-center gap-x-6 gap-y-2 mt-8 text-sm text-muted-foreground">
            <span className="flex items-center gap-1.5">
              <span className="size-1.5 rounded-full bg-success" />
              HTTP / HTTPS / FTP
            </span>
            <span className="flex items-center gap-1.5">
              <span className="size-1.5 rounded-full bg-primary" />
              BitTorrent &amp; magnets
            </span>
            <span className="flex items-center gap-1.5">
              <span className="size-1.5 rounded-full bg-accent" />
              JWT-secured API
            </span>
          </div>
        </div>

        <div
          className="mt-20 max-w-4xl mx-auto animate-fade-up"
          style={{ animationDelay: "240ms" }}
        >
          <DashboardPreview />
        </div>
      </div>
    </section>
  );
};

export default Hero;
