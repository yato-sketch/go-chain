import { useLocation } from "react-router-dom";
import ToolBar from "./ToolBar";
import { Navbar } from "./Navbar";
import { SidebarInset, SidebarProvider, SidebarTrigger } from "@/components/ui/sidebar";

function viewMeta(pathname: string): { title: string; subtitle: string } {
  const p = pathname.replace(/\/$/, "") || "/";
  if (p === "" || p === "/") {
    return { title: "Overview", subtitle: "Balances, default address, and chain status" };
  }
  if (p === "/social" || p.startsWith("/social/")) {
    return { title: "Social", subtitle: "Wallet IRC — community channel" };
  }
  return { title: "Wallet", subtitle: "Fairchain" };
}

export default function MainLayout({ children }: { children: React.ReactNode }) {
  const { pathname } = useLocation();
  const { title, subtitle } = viewMeta(pathname);

  return (
    <SidebarProvider className="flex h-full min-h-0! w-full flex-col">
      <header
        role="menubar"
        className="relative z-30 flex h-9 min-h-(--app-menubar-height) shrink-0 items-stretch border-b"
        style={{
          borderColor: "var(--color-btc-border)",
          background: "var(--color-btc-surface)",
        }}
      >
        <div
          className="flex shrink-0 items-center border-r px-0.5"
          style={{ borderColor: "var(--color-btc-border)" }}
        >
          <SidebarTrigger
            className="shrink-0"
            style={{ color: "var(--color-btc-text-muted)" }}
          />
        </div>
        <ToolBar />
      </header>

      <div className="flex min-h-0 flex-1 flex-row overflow-hidden">
        <Navbar />
        <SidebarInset className="min-h-0 flex-1 overflow-hidden border-0 bg-transparent md:peer-data-[variant=inset]:m-0 md:peer-data-[variant=inset]:shadow-none">
          <div
            className="flex min-h-0 flex-1 flex-col overflow-hidden"
            style={{ background: "var(--color-btc-deep)" }}
          >
            <div
              className="flex shrink-0 flex-col justify-center border-b px-5 py-2.5 md:px-6"
              style={{
                borderColor: "var(--color-btc-border)",
                background: "var(--color-btc-card)",
              }}
            >
              <h1
                className="text-[15px] font-semibold leading-tight tracking-tight"
                style={{ color: "var(--color-btc-text)" }}
              >
                {title}
              </h1>
              <p
                className="mt-0.5 text-[11px] leading-snug"
                style={{ color: "var(--color-btc-text-muted)" }}
              >
                {subtitle}
              </p>
            </div>
            <div className="min-h-0 flex-1 overflow-y-auto overflow-x-hidden overscroll-contain p-5 md:p-6">
              {children}
            </div>
          </div>
        </SidebarInset>
      </div>
    </SidebarProvider>
  );
}
