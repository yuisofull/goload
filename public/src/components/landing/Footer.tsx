import { Cloud, Github, Globe } from "lucide-react";

const Footer = () => {
  return (
    <footer className="border-t border-border/60 py-12 mt-12">
      <div className="container max-w-[1200px] flex flex-col md:flex-row items-center justify-between gap-6">
        <div className="flex items-center gap-3">
          <span className="grid place-items-center size-7 rounded-lg bg-gradient-primary">
            <Cloud className="size-3.5 text-primary-foreground" strokeWidth={2.5} />
          </span>
          <div className="flex flex-col">
            <span className="font-display font-semibold text-sm">Goload</span>
            <span className="text-xs text-muted-foreground">© 2024 · Invisible power for your data.</span>
          </div>
        </div>

        <nav className="flex flex-wrap justify-center gap-6 text-xs text-muted-foreground">
          <a className="hover:text-foreground transition-colors" href="#">Documentation</a>
          <a className="hover:text-foreground transition-colors" href="#">API</a>
          <a className="hover:text-foreground transition-colors" href="#">Status</a>
          <a className="hover:text-foreground transition-colors" href="#">Privacy</a>
          <a className="hover:text-foreground transition-colors" href="#">Terms</a>
        </nav>

        <div className="flex items-center gap-2">
          <a href="https://github.com/yuisofull/goload" target="_blank" rel="noreferrer" className="size-9 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors">
            <Github className="size-4" />
          </a>
          <button className="size-9 grid place-items-center rounded-full text-muted-foreground hover:text-foreground hover:bg-secondary transition-colors">
            <Globe className="size-4" />
          </button>
        </div>
      </div>
    </footer>
  );
};

export default Footer;
