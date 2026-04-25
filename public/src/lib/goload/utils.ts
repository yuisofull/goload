// Utility helpers for the Goload UI.

export const formatBytes = (bytes?: number | null): string => {
  if (bytes == null || isNaN(bytes) || bytes <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let value = bytes;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${value.toFixed(value >= 100 || unit === 0 ? 0 : 1)} ${units[unit]}`;
};

export const isMagnetUri = (url: string): boolean =>
  /^magnet:\?/i.test(url.trim());

export const isTorrentUrl = (url: string): boolean =>
  /\.torrent($|\?)/i.test(url.trim());

export const isBitTorrentSource = (url: string): boolean =>
  isMagnetUri(url) || isTorrentUrl(url);

export const fileNameFromMagnet = (uri: string): string | null => {
  const match = uri.match(/[?&]dn=([^&]+)/i);
  if (!match) return null;
  try {
    return decodeURIComponent(match[1].replace(/\+/g, " "));
  } catch {
    return match[1];
  }
};

export const fileNameFromUrl = (url: string): string => {
  if (isMagnetUri(url)) {
    return fileNameFromMagnet(url) ?? "torrent-download";
  }
  try {
    const u = new URL(url);
    const last = u.pathname.split("/").filter(Boolean).pop();
    if (last && last.includes(".")) {
      const name = decodeURIComponent(last);
      // Strip .torrent extension — the actual payload is the contained file(s).
      return name.replace(/\.torrent$/i, "") || name;
    }
    return u.hostname.replace(/^www\./, "") + ".bin";
  } catch {
    return "download.bin";
  }
};

export const sourceTypeFromUrl = (url: string): string => {
  if (isBitTorrentSource(url)) return "BITTORRENT";
  try {
    const u = new URL(url);
    const proto = u.protocol.replace(":", "").toLowerCase();
    if (proto === "http") return "HTTP";
    if (proto === "https") return "HTTPS";
    if (proto === "ftp") return "FTP";
    return proto.toUpperCase();
  } catch {
    return "HTTPS";
  }
};

/** Read a File as base64 (without the data:...;base64, prefix). */
export const fileToBase64 = (file: File): Promise<string> =>
  new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onerror = () => reject(reader.error ?? new Error("Read failed"));
    reader.onload = () => {
      const result = reader.result;
      if (typeof result !== "string") {
        reject(new Error("Unexpected reader result"));
        return;
      }
      const comma = result.indexOf(",");
      resolve(comma >= 0 ? result.slice(comma + 1) : result);
    };
    reader.readAsDataURL(file);
  });

export const statusTone = (status: string): "primary" | "success" | "warning" | "destructive" | "muted" => {
  const s = status.toLowerCase();
  if (s.includes("complete") || s === "ready" || s === "done") return "success";
  if (s.includes("download") || s.includes("active") || s.includes("processing")) return "primary";
  if (s.includes("paus") || s.includes("queue") || s.includes("pending")) return "warning";
  if (s.includes("fail") || s.includes("cancel") || s.includes("error")) return "destructive";
  return "muted";
};
