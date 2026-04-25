import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from "react";
import {
  createAccount as apiCreateAccount,
  createSession as apiCreateSession,
  extractError,
  getStoredAccount,
  getStoredToken,
  setStoredAccount,
  setStoredToken,
  setUnauthorizedHandler,
} from "@/lib/goload/client";
import type { AuthAccount } from "@/lib/goload/types";

interface AuthContextValue {
  token: string | null;
  account: AuthAccount | null;
  isAuthenticated: boolean;
  signIn: (account_name: string, password: string) => Promise<void>;
  signUp: (account_name: string, password: string) => Promise<void>;
  signOut: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export const AuthProvider = ({ children }: { children: React.ReactNode }) => {
  const [token, setToken] = useState<string | null>(() => getStoredToken());
  const [account, setAccount] = useState<AuthAccount | null>(() => getStoredAccount());

  const signOut = useCallback(() => {
    setStoredToken(null);
    setStoredAccount(null);
    setToken(null);
    setAccount(null);
  }, []);

  useEffect(() => {
    setUnauthorizedHandler(() => signOut());
    return () => setUnauthorizedHandler(null);
  }, [signOut]);

  const signIn = useCallback(async (account_name: string, password: string) => {
    try {
      const res = await apiCreateSession(account_name, password);
      setStoredToken(res.token);
      setStoredAccount(res.account);
      setToken(res.token);
      setAccount(res.account);
    } catch (err) {
      throw new Error(extractError(err));
    }
  }, []);

  const signUp = useCallback(async (account_name: string, password: string) => {
    try {
      await apiCreateAccount(account_name, password);
      // Auto sign-in after registration.
      const res = await apiCreateSession(account_name, password);
      setStoredToken(res.token);
      setStoredAccount(res.account);
      setToken(res.token);
      setAccount(res.account);
    } catch (err) {
      throw new Error(extractError(err));
    }
  }, []);

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      account,
      isAuthenticated: Boolean(token),
      signIn,
      signUp,
      signOut,
    }),
    [token, account, signIn, signUp, signOut]
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
};

// eslint-disable-next-line react-refresh/only-export-components
export const useAuth = () => {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within an AuthProvider");
  return ctx;
};
