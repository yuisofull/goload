import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { Cloud, LogOut, LayoutDashboard } from "lucide-react";
import { useAuth } from "@/contexts/AuthContext";
import { useAuthModal } from "@/contexts/AuthModalContext";

const Header = () => {
  const { isAuthenticated, account, signOut } = useAuth();
  const { openModal } = useAuthModal();
  const navigate = useNavigate();
  const [menuOpen, setMenuOpen] = useState(false);

  return (
    <header className="fixed top-0 inset-x-0 z-50 bg-background/75 backdrop-blur-xl border-b border-border/40">
      <div className="container max-w-[1200px] flex items-center justify-between py-4">
        <div className="flex items-center gap-10">
          <Link to="/" className="flex items-center gap-2 group">
            <span className="grid place-items-center size-8 rounded-xl bg-gradient-primary shadow-glow ease-soft transition-transform group-hover:scale-105">
              <Cloud className="size-4 text-primary-foreground" strokeWidth={2.5} />
            </span>
            <span className="font-display text-lg font-bold tracking-tight">Goload</span>
          </Link>
          <nav className="hidden md:flex items-center gap-8 text-sm">
            <a href="/#features" className="text-muted-foreground hover:text-foreground transition-colors">Features</a>
            <a href="/#how" className="text-muted-foreground hover:text-foreground transition-colors">How it works</a>
            <a href="/#security" className="text-muted-foreground hover:text-foreground transition-colors">Security</a>
            <a href="https://github.com/yuisofull/goload" target="_blank" rel="noreferrer" className="text-muted-foreground hover:text-foreground transition-colors">GitHub</a>
          </nav>
        </div>

        <div className="flex items-center gap-2">
          {isAuthenticated ? (
            <div className="relative">
              <button
                onClick={() => setMenuOpen((v) => !v)}
                className="inline-flex items-center gap-2 h-10 px-3 rounded-full bg-secondary hover:bg-muted transition-colors"
              >
                <span className="grid place-items-center size-7 rounded-full bg-gradient-primary text-primary-foreground text-xs font-bold">
                  {account?.account_name?.[0]?.toUpperCase() ?? "U"}
                </span>
                <span className="text-sm font-medium hidden sm:inline pr-1">
                  {account?.account_name}
                </span>
              </button>
              {menuOpen && (
                <>
                  <div className="fixed inset-0 z-40" onClick={() => setMenuOpen(false)} />
                  <div className="absolute right-0 top-12 z-50 w-56 bg-card rounded-2xl shadow-elevated border border-border/40 p-1.5 animate-in fade-in zoom-in-95 duration-150">
                    <button
                      onClick={() => { setMenuOpen(false); navigate("/dashboard"); }}
                      className="w-full flex items-center gap-2.5 px-3 py-2 rounded-xl text-sm text-foreground hover:bg-secondary transition-colors"
                    >
                      <LayoutDashboard className="size-4 text-muted-foreground" />
                      Dashboard
                    </button>
                    <div className="h-px bg-border/60 my-1" />
                    <button
                      onClick={() => { setMenuOpen(false); signOut(); navigate("/"); }}
                      className="w-full flex items-center gap-2.5 px-3 py-2 rounded-xl text-sm text-foreground hover:bg-secondary transition-colors"
                    >
                      <LogOut className="size-4 text-muted-foreground" />
                      Sign out
                    </button>
                  </div>
                </>
              )}
            </div>
          ) : (
            <>
              <button
                onClick={() => openModal({ mode: "signin" })}
                className="hidden sm:inline-flex h-10 px-4 items-center text-sm font-medium text-muted-foreground hover:text-foreground transition-colors"
              >
                Log In
              </button>
              <button
                onClick={() => openModal({ mode: "signup" })}
                className="inline-flex h-10 px-5 items-center rounded-full bg-foreground text-background text-sm font-medium hover:bg-foreground/90 active:scale-[0.97] transition-all ease-soft"
              >
                Get Started
              </button>
            </>
          )}
        </div>
      </div>
    </header>
  );
};

export default Header;
