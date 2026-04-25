import { Gauge, RefreshCw, Globe2, FolderLock, Workflow, Webhook } from "lucide-react";

const features = [
  {
    icon: Gauge,
    title: "Line-rate transfers",
    desc: "Multi-threaded fetchers saturate your storage tier — gigabytes in seconds.",
  },
  {
    icon: RefreshCw,
    title: "Resumable by default",
    desc: "Network blip? We pick up exactly where we left off. No corruption, no retries.",
  },
  {
    icon: Globe2,
    title: "Global edge fleet",
    desc: "Workers run close to the source so origin throttling can't slow you down.",
  },
  {
    icon: FolderLock,
    title: "Private vaults",
    desc: "Your downloads land in an isolated, per-tenant object store. Always.",
  },
  {
    icon: Workflow,
    title: "Bulk pipelines",
    desc: "Queue thousands of links via UI, CLI, or API. We handle the orchestration.",
  },
  {
    icon: Webhook,
    title: "Webhooks & API",
    desc: "Trigger downstream pipelines the moment a file lands in your vault.",
  },
];

const Features = () => {
  return (
    <section id="features" className="py-32 bg-secondary/30">
      <div className="container max-w-[1200px]">
        <div className="text-center max-w-2xl mx-auto mb-20">
          <span className="inline-block text-xs font-semibold tracking-[0.2em] text-primary uppercase mb-4">
            Built for power users
          </span>
          <h2 className="font-display text-4xl md:text-5xl font-bold tracking-tight text-balance mb-4">
            Everything a download<br />manager should be.
          </h2>
          <p className="text-muted-foreground text-lg">
            Engineered in Go. Tuned for throughput. Designed to disappear.
          </p>
        </div>

        <div className="grid sm:grid-cols-2 lg:grid-cols-3 gap-px bg-border/40 rounded-[28px] overflow-hidden border border-border/40">
          {features.map((feature) => {
            const Icon = feature.icon;
            return (
              <div
                key={feature.title}
                className="group p-8 bg-card hover:bg-surface-low transition-colors duration-300"
              >
                <div className="inline-flex items-center justify-center size-12 rounded-2xl bg-primary-soft text-primary mb-6 group-hover:bg-gradient-primary group-hover:text-primary-foreground group-hover:shadow-glow transition-all duration-500 ease-soft">
                  <Icon className="size-5" strokeWidth={2.2} />
                </div>
                <h3 className="font-display text-lg font-semibold tracking-tight mb-2">
                  {feature.title}
                </h3>
                <p className="text-muted-foreground text-sm leading-relaxed">
                  {feature.desc}
                </p>
              </div>
            );
          })}
        </div>
      </div>
    </section>
  );
};

export default Features;
