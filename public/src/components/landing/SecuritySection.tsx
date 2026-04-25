import { ShieldCheck, Lock, KeyRound, CheckCircle2 } from "lucide-react";

const SecuritySection = () => {
  return (
    <section id="security" className="py-24">
      <div className="container max-w-[1200px]">
        <div className="relative overflow-hidden rounded-[40px] bg-inverse text-inverse-foreground p-10 md:p-20">
          {/* Ambient glows */}
          <div className="absolute -right-20 -top-20 w-[500px] h-[500px] bg-primary/30 rounded-full blur-[120px] pointer-events-none" />
          <div className="absolute -left-20 -bottom-32 w-[400px] h-[400px] bg-accent/20 rounded-full blur-[120px] pointer-events-none" />
          <div className="absolute inset-0 opacity-[0.03] pointer-events-none" style={{
            backgroundImage: 'radial-gradient(circle at 1px 1px, white 1px, transparent 0)',
            backgroundSize: '32px 32px',
          }} />

          <div className="relative z-10 grid md:grid-cols-2 gap-16 items-center">
            <div>
              <div className="inline-flex items-center gap-2 px-3 py-1.5 bg-white/10 rounded-full mb-8 backdrop-blur-md border border-white/10">
                <ShieldCheck className="size-3.5 text-primary-glow" />
                <span className="text-xs font-medium tracking-tight">Privacy-first architecture</span>
              </div>
              <h2 className="font-display text-4xl md:text-5xl font-bold tracking-tight mb-6 leading-[1.1] text-balance">
                Secure & private<br />by design.
              </h2>
              <p className="text-inverse-foreground/70 text-lg mb-10 leading-relaxed max-w-md">
                Files live in isolated object storage with per-tenant encryption.
                We generate time-limited access links — your data stays exactly
                where it belongs.
              </p>

              <ul className="space-y-3">
                {[
                  "End-to-end encryption (AES-256)",
                  "Zero-knowledge vault architecture",
                  "Time-limited signed access links",
                  "SOC 2 compliant infrastructure",
                ].map((item) => (
                  <li key={item} className="flex items-center gap-3 text-sm">
                    <span className="grid place-items-center size-5 rounded-full bg-success/20">
                      <CheckCircle2 className="size-3.5 text-success" />
                    </span>
                    <span>{item}</span>
                  </li>
                ))}
              </ul>
            </div>

            {/* Visual */}
            <div className="flex justify-center md:justify-end">
              <div className="relative w-full max-w-sm aspect-square">
                <div className="absolute inset-0 rounded-[40px] bg-gradient-to-tr from-white/5 to-white/[0.02] border border-white/10 backdrop-blur-sm" />
                <div className="absolute inset-8 rounded-[32px] bg-gradient-to-br from-primary/20 to-accent/10 border border-white/5 grid place-items-center">
                  <div className="relative">
                    <div className="absolute inset-0 bg-primary blur-2xl opacity-50" />
                    <div className="relative size-28 rounded-3xl bg-gradient-primary grid place-items-center shadow-glow">
                      <Lock className="size-12 text-primary-foreground" strokeWidth={1.8} />
                    </div>
                  </div>
                </div>
                {/* Floating badges */}
                <div className="absolute top-4 -right-2 px-3 py-2 rounded-2xl bg-white/10 backdrop-blur-md border border-white/10 flex items-center gap-2 animate-pulse-soft">
                  <KeyRound className="size-3.5 text-primary-glow" />
                  <span className="text-xs font-medium">256-bit</span>
                </div>
                <div className="absolute bottom-8 -left-3 px-3 py-2 rounded-2xl bg-white/10 backdrop-blur-md border border-white/10 flex items-center gap-2">
                  <ShieldCheck className="size-3.5 text-success" />
                  <span className="text-xs font-medium">Verified</span>
                </div>
              </div>
            </div>
          </div>
        </div>
      </div>
    </section>
  );
};

export default SecuritySection;
