import { useWalletChainStatus } from "@/hooks/useWalletChainStatus";

const MENU_ITEMS = ["File", "Settings", "Help"] as const;

export default function ToolBar() {
  const { height, network, peers } = useWalletChainStatus();

  return (
    <div className="flex min-w-0 flex-1 items-center justify-between gap-3 pr-2">
      <nav
        className="flex min-w-0 items-center gap-0.5 px-1.5"
        aria-label="Application menu"
      >
        {MENU_ITEMS.map((label) => (
          <button
            key={label}
            type="button"
            className="rounded-md px-2.5 py-1 text-left text-[13px] font-medium tracking-tight outline-none transition-colors hover:bg-(--color-overlay-hover-strong) focus-visible:ring-2 focus-visible:ring-(--color-btc-gold)/35"
            style={{ color: "var(--color-btc-text-muted)" }}
          >
            {label}
          </button>
        ))}
      </nav>

      <div
        className="flex shrink-0 items-center gap-2.5 border-l pl-2.5 text-[11px] tabular-nums"
        style={{
          borderColor: "var(--color-btc-border)",
          color: "var(--color-btc-text-muted)",
        }}
        aria-live="polite"
        aria-label="Network and chain status"
      >
        {network ? (
          <span
            className="inline-block max-w-28 truncate rounded px-1.5 py-0.5 font-medium"
            style={{
              background: "rgba(247, 147, 26, 0.1)",
              color: "var(--color-btc-gold)",
              border: "1px solid rgba(247, 147, 26, 0.2)",
            }}
            title={network}
          >
            {network}
          </span>
        ) : null}
        <div className="flex items-center gap-1">
          <div
            className="h-1.5 w-1.5 shrink-0 rounded-full"
            style={{ background: peers > 0 ? "var(--color-btc-green)" : "var(--color-btc-red)" }}
          />
          <span>
            {peers} peer{peers !== 1 ? "s" : ""}
          </span>
        </div>
        <span className="font-mono" style={{ color: "var(--color-btc-text-dim)" }}>
          #{height.toLocaleString()}
        </span>
      </div>
    </div>
  );
}
