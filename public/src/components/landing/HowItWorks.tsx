import { Link2, Zap, Cloud } from "lucide-react";

const steps = [
  {
    icon: Link2,
    title: "Submit",
    desc: "Paste any HTTP / HTTPS link from the web. Our edge servers catch it instantly — no plugins, no waiting.",
  },
  {
    icon: Zap,
    title: "Process",
    desc: "Our distributed engine fetches the file at line-rate speed, regardless of size or origin throttling.",
  },
  {
    icon: Cloud,
    title: "Access",
    desc: "Stream or share your file from your private vault. Time-limited links, end-to-end encrypted.",
  },
];

const HowItWorks = () => {
  return (
    <section id="how" className="py-32">
      <div className="container max-w-[1200px]">
        <div className="text-center max-w-2xl mx-auto mb-20">
          <span className="inline-block text-xs font-semibold tracking-[0.2em] text-primary uppercase mb-4">
            How it works
          </span>
          <h2 className="font-display text-4xl md:text-5xl font-bold tracking-tight text-balance">
            Three steps. Zero friction.
          </h2>
        </div>

        <div className="grid md:grid-cols-3 gap-6 relative">
          {/* Connecting line */}
          <div className="hidden md:block absolute top-12 left-[16%] right-[16%] h-px bg-gradient-to-r from-transparent via-border to-transparent" />

          {steps.map((step, idx) => {
            const Icon = step.icon;
            return (
              <div
                key={step.title}
                className="relative p-8 bg-card rounded-[24px] border border-border/40 shadow-soft hover:shadow-elevated hover:-translate-y-1 transition-all duration-500 ease-soft"
              >
                <div className="relative inline-flex items-center justify-center size-14 rounded-2xl bg-gradient-primary shadow-glow mb-6">
                  <Icon className="size-6 text-primary-foreground" strokeWidth={2.2} />
                </div>
                <div className="absolute top-8 right-8 font-display text-5xl font-bold text-secondary">
                  0{idx + 1}
                </div>
                <h3 className="font-display text-xl font-semibold mb-3 tracking-tight">
                  {step.title}
                </h3>
                <p className="text-muted-foreground leading-relaxed">
                  {step.desc}
                </p>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
};

export default HowItWorks;
