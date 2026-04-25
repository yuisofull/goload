import { createContext, useCallback, useContext, useMemo, useState } from "react";

type Mode = "signin" | "signup";

interface AuthModalState {
  open: boolean;
  mode: Mode;
  pendingUrl: string | null;
}

interface AuthModalValue extends AuthModalState {
  openModal: (opts?: { mode?: Mode; pendingUrl?: string | null }) => void;
  closeModal: () => void;
  setMode: (m: Mode) => void;
  consumePendingUrl: () => string | null;
}

const AuthModalContext = createContext<AuthModalValue | undefined>(undefined);

export const AuthModalProvider = ({ children }: { children: React.ReactNode }) => {
  const [state, setState] = useState<AuthModalState>({
    open: false,
    mode: "signin",
    pendingUrl: null,
  });

  const openModal = useCallback<AuthModalValue["openModal"]>((opts) => {
    setState((s) => ({
      open: true,
      mode: opts?.mode ?? s.mode,
      pendingUrl: opts?.pendingUrl ?? s.pendingUrl,
    }));
  }, []);

  const closeModal = useCallback(() => {
    setState((s) => ({ ...s, open: false }));
  }, []);

  const setMode = useCallback((mode: Mode) => {
    setState((s) => ({ ...s, mode }));
  }, []);

  const consumePendingUrl = useCallback(() => {
    let url: string | null = null;
    setState((s) => {
      url = s.pendingUrl;
      return { ...s, pendingUrl: null };
    });
    return url;
  }, []);

  const value = useMemo<AuthModalValue>(
    () => ({ ...state, openModal, closeModal, setMode, consumePendingUrl }),
    [state, openModal, closeModal, setMode, consumePendingUrl]
  );

  return <AuthModalContext.Provider value={value}>{children}</AuthModalContext.Provider>;
};

// eslint-disable-next-line react-refresh/only-export-components
export const useAuthModal = () => {
  const ctx = useContext(AuthModalContext);
  if (!ctx) throw new Error("useAuthModal must be used within AuthModalProvider");
  return ctx;
};
