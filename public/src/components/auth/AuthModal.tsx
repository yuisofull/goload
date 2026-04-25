import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { Cloud, Loader2, X } from "lucide-react";
import { useAuth } from "@/contexts/AuthContext";
import { useAuthModal } from "@/contexts/AuthModalContext";
import {
  createTask,
  extractError,
} from "@/lib/goload/client";
import { fileNameFromUrl, sourceTypeFromUrl } from "@/lib/goload/utils";
import { toast } from "sonner";

const Field = ({
  label,
  ...props
}: React.InputHTMLAttributes<HTMLInputElement> & { label: string }) => (
  <label className="block">
    <span className="block text-xs font-medium text-muted-foreground mb-1.5">
      {label}
    </span>
    <input
      {...props}
      className="w-full h-11 px-4 rounded-xl bg-secondary/60 border border-border/60 text-sm outline-none focus:border-primary focus:ring-2 focus:ring-primary/20 transition-all"
    />
  </label>
);

const AuthModal = () => {
  const { open, mode, closeModal, setMode, consumePendingUrl } = useAuthModal();
  const { signIn, signUp } = useAuth();
  const navigate = useNavigate();

  const [accountName, setAccountName] = useState("");
  const [password, setPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setError(null);
      setLoading(false);
    }
  }, [open]);

  const handleAfterAuth = async () => {
    const pending = consumePendingUrl();
    if (pending) {
      try {
        const task = await createTask({
          file_name: fileNameFromUrl(pending),
          source_url: pending,
          source_type: sourceTypeFromUrl(pending),
        });
        toast.success("Download queued", {
          description: task.file_name,
        });
      } catch (err) {
        toast.error("Could not start download", {
          description: extractError(err),
        });
      }
    }
    navigate("/dashboard");
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(null);

    if (!accountName.trim() || !password) {
      setError("Account name and password are required.");
      return;
    }
    if (mode === "signup") {
      if (password.length < 6) {
        setError("Password must be at least 6 characters.");
        return;
      }
      if (password !== confirmPassword) {
        setError("Passwords do not match.");
        return;
      }
    }

    setLoading(true);
    try {
      if (mode === "signin") await signIn(accountName.trim(), password);
      else await signUp(accountName.trim(), password);
      closeModal();
      setAccountName("");
      setPassword("");
      setConfirmPassword("");
      await handleAfterAuth();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  if (!open) return null;

  return (
    <div
      className="fixed inset-0 z-[100] flex items-center justify-center p-4 bg-foreground/30 backdrop-blur-md animate-in fade-in duration-200"
      onClick={closeModal}
    >
      <div
        onClick={(e) => e.stopPropagation()}
        className="relative w-full max-w-[420px] bg-card rounded-[24px] shadow-elevated border border-border/40 p-8 animate-in zoom-in-95 duration-200"
      >
        <button
          type="button"
          onClick={closeModal}
          className="absolute top-5 right-5 size-8 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors"
          aria-label="Close"
        >
          <X className="size-4" />
        </button>

        <div className="flex items-center gap-2.5 mb-1">
          <span className="grid place-items-center size-8 rounded-xl bg-gradient-primary shadow-glow">
            <Cloud className="size-4 text-primary-foreground" strokeWidth={2.5} />
          </span>
          <span className="font-display font-semibold text-sm">Goload</span>
        </div>
        <h2 className="font-display text-2xl font-bold tracking-tight mb-1">
          {mode === "signin" ? "Welcome back" : "Create your account"}
        </h2>
        <p className="text-sm text-muted-foreground mb-6">
          {mode === "signin"
            ? "Sign in to your private vault."
            : "Start downloading at line-rate speed."}
        </p>

        {/* Tab toggle */}
        <div className="flex gap-1 p-1 rounded-xl bg-secondary/60 mb-6">
          <button
            type="button"
            onClick={() => setMode("signin")}
            className={`flex-1 h-9 rounded-lg text-sm font-medium transition-all ${
              mode === "signin"
                ? "bg-card text-foreground shadow-soft"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Sign In
          </button>
          <button
            type="button"
            onClick={() => setMode("signup")}
            className={`flex-1 h-9 rounded-lg text-sm font-medium transition-all ${
              mode === "signup"
                ? "bg-card text-foreground shadow-soft"
                : "text-muted-foreground hover:text-foreground"
            }`}
          >
            Create Account
          </button>
        </div>

        <form onSubmit={handleSubmit} className="space-y-4">
          <Field
            label="Account name"
            type="text"
            autoComplete="username"
            placeholder="alex"
            value={accountName}
            onChange={(e) => setAccountName(e.target.value)}
            disabled={loading}
            autoFocus
          />
          <Field
            label="Password"
            type="password"
            autoComplete={mode === "signin" ? "current-password" : "new-password"}
            placeholder="••••••••"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            disabled={loading}
          />
          {mode === "signup" && (
            <Field
              label="Confirm password"
              type="password"
              autoComplete="new-password"
              placeholder="••••••••"
              value={confirmPassword}
              onChange={(e) => setConfirmPassword(e.target.value)}
              disabled={loading}
            />
          )}

          {error && (
            <div className="text-sm text-destructive bg-destructive/10 border border-destructive/20 rounded-xl px-3 py-2">
              {error}
            </div>
          )}

          <button
            type="submit"
            disabled={loading}
            className="w-full h-12 rounded-xl bg-gradient-primary text-primary-foreground font-medium shadow-glow hover:shadow-elevated active:scale-[0.98] transition-all ease-soft disabled:opacity-60 disabled:cursor-not-allowed flex items-center justify-center gap-2"
          >
            {loading && <Loader2 className="size-4 animate-spin" />}
            {mode === "signin" ? "Sign In" : "Create Account"}
          </button>
        </form>

        <p className="text-xs text-center text-muted-foreground mt-6">
          {mode === "signin" ? (
            <>
              Don't have an account?{" "}
              <button
                type="button"
                onClick={() => setMode("signup")}
                className="text-primary font-medium hover:underline"
              >
                Register
              </button>
            </>
          ) : (
            <>
              Already have an account?{" "}
              <button
                type="button"
                onClick={() => setMode("signin")}
                className="text-primary font-medium hover:underline"
              >
                Sign in
              </button>
            </>
          )}
        </p>
      </div>
    </div>
  );
};

export default AuthModal;
