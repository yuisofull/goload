import { ArrowRight } from "lucide-react";
import { useNavigate } from "react-router-dom";
import { useAuth } from "@/contexts/AuthContext";
import { useAuthModal } from "@/contexts/AuthModalContext";

const CTA = () => {
  const { isAuthenticated } = useAuth();
  const { openModal } = useAuthModal();
  const navigate = useNavigate();

  const handlePrimary = () => {
    if (isAuthenticated) navigate("/dashboard");
    else openModal({ mode: "signup" });
  };

  return (
    <section className="py-24">
      <div className="container max-w-[1200px]">
        <div className="relative overflow-hidden rounded-[40px] bg-card border border-border/40 shadow-elevated p-12 md:p-20 text-center">
          <div className="absolute inset-0 bg-gradient-hero opacity-60 pointer-events-none" />
          <div className="relative">
            <h2 className="font-display text-4xl md:text-6xl font-bold tracking-tight mb-6 text-balance">
              Stop babysitting downloads.<br />
              <span className="text-muted-foreground">Start using Goload.</span>
            </h2>
            <p className="text-muted-foreground text-lg max-w-xl mx-auto mb-10">
              Free, self-hosted, and built in Go. Set up in under a minute.
            </p>
            <div className="flex flex-wrap justify-center gap-3">
              <button
                onClick={handlePrimary}
                className="inline-flex items-center gap-2 h-12 px-7 rounded-full bg-gradient-primary text-primary-foreground font-medium shadow-glow hover:shadow-elevated active:scale-[0.98] transition-all ease-soft"
              >
                {isAuthenticated ? "Open dashboard" : "Get started — it's free"}
                <ArrowRight className="size-4" />
              </button>
              <a
                href="https://github.com/yuisofull/goload"
                target="_blank"
                rel="noreferrer"
                className="inline-flex items-center h-12 px-7 rounded-full bg-card border border-border text-foreground font-medium hover:bg-secondary active:scale-[0.98] transition-all ease-soft"
              >
                View source
              </a>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

export default CTA;
