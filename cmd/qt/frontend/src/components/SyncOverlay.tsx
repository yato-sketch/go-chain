import { useEffect, useRef, useState, type ReactNode } from "react";
import { AlertTriangle } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { Separator } from "@/components/ui/separator";
import { useCoinInfo } from "../hooks/useCoinInfo";
import { GetSyncStatus } from "../../wailsjs/go/main/App";

interface SyncStatus {
  syncState: string;
  headerHeight: number;
  blockHeight: number;
  bestPeerHeight: number;
  peers: number;
  progress: number;
  lastBlockTime: number;
}

function formatBlockTime(unix: number): string {
  if (!unix || unix <= 0) return "Unknown";
  const d = new Date(unix * 1000);
  return d.toLocaleDateString(undefined, {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
    second: "2-digit",
  });
}

function formatEta(secondsLeft: number): string {
  if (!isFinite(secondsLeft) || secondsLeft <= 0) return "Unknown...";
  if (secondsLeft < 60) return "< 1 minute";
  if (secondsLeft < 3600) {
    const m = Math.ceil(secondsLeft / 60);
    return `${m} minute${m !== 1 ? "s" : ""}`;
  }
  const h = Math.floor(secondsLeft / 3600);
  const m = Math.ceil((secondsLeft % 3600) / 60);
  return `${h}h ${m}m`;
}

export function SyncOverlay({ onHide }: { onHide: () => void }) {
  const coinInfo = useCoinInfo();
  const [status, setStatus] = useState<SyncStatus | null>(null);

  const progressHistory = useRef<{ time: number; progress: number }[]>([]);
  const [ratePerHour, setRatePerHour] = useState<number | null>(null);
  const [eta, setEta] = useState<string>("Unknown...");

  useEffect(() => {
    const poll = () => {
      GetSyncStatus()
        .then((s) => {
          const st = s as unknown as SyncStatus;
          setStatus(st);

          const now = Date.now();
          const hist = progressHistory.current;
          hist.push({ time: now, progress: st.progress });

          // Keep only the last 60 seconds of samples for rate calculation.
          const cutoff = now - 60_000;
          while (hist.length > 1 && hist[0].time < cutoff) {
            hist.shift();
          }

          if (hist.length >= 2) {
            const oldest = hist[0];
            const elapsed = (now - oldest.time) / 1000;
            const delta = st.progress - oldest.progress;
            if (elapsed > 5 && delta > 0) {
              const perSecond = delta / elapsed;
              setRatePerHour(perSecond * 3600);
              const remaining = 1.0 - st.progress;
              setEta(formatEta(remaining / perSecond));
            } else if (delta <= 0) {
              setRatePerHour(null);
              setEta("calculating...");
            }
          } else {
            setRatePerHour(null);
            setEta("calculating...");
          }
        })
        .catch(() => {});
    };
    poll();
    const id = setInterval(poll, 1500);
    return () => clearInterval(id);
  }, []);

  const blocksLeft = status
    ? status.bestPeerHeight > status.blockHeight
      ? status.bestPeerHeight - status.blockHeight
      : 0
    : 0;

  const isHeaderSync = status?.syncState === "HEADER_SYNC";
  const progressPct = status ? (status.progress * 100).toFixed(2) : "0.00";
  const progressWidth = Math.min(100, status?.progress ? status.progress * 100 : 0);

  return (
    <div className="btc-noise absolute inset-0 z-50 flex flex-col bg-(--color-btc-deep)">
      {/* Warning banner */}
      <Card className="rounded-none border-x-0 border-t-0 border-(--color-btc-border) bg-(--color-btc-surface) shadow-none ring-0">
        <CardHeader className="flex flex-row items-start gap-3 px-6 py-4 sm:gap-4">
          <AlertTriangle
            className="mt-0.5 size-6 shrink-0 text-(--color-btc-gold)"
            aria-hidden
          />
          <div className="min-w-0 flex-1 space-y-3">
            <div className="flex flex-wrap items-center gap-2 sm:justify-between">
              <CardTitle className="text-base leading-snug">Synchronizing wallet</CardTitle>
              <Badge
                variant="outline"
                className="border-(--color-btc-gold)/35 text-(--color-btc-gold)"
              >
                {status == null ? "Connecting" : isHeaderSync ? "Header sync" : "Block sync"}
              </Badge>
            </div>
            <CardDescription className="text-(--color-btc-text)">
              <p className="text-sm leading-relaxed">
                Recent transactions may not yet be visible, and therefore your wallet&apos;s balance
                might be incorrect. This information will be correct once your wallet has finished
                synchronizing with the {coinInfo.name} network, as detailed below.
              </p>
              <p className="mt-2 text-sm font-semibold leading-relaxed text-foreground">
                Attempting to spend {coinInfo.nameLower} that are affected by not-yet-displayed
                transactions will not be accepted by the network.
              </p>
            </CardDescription>
          </div>
        </CardHeader>
      </Card>

      {/* Sync detail */}
      <div className="flex flex-1 items-center justify-center p-6 sm:p-8">
        <Card className="btc-glow w-full max-w-lg border-(--color-btc-border) bg-(--color-btc-card)">
          <CardHeader className="border-b border-(--color-btc-border) pb-4">
            <CardTitle className="text-base">Sync status</CardTitle>
            <CardDescription>Live progress from your node.</CardDescription>
          </CardHeader>
          <CardContent className="space-y-0 px-0 pb-2 pt-0">
            <StatRow
              label="Number of blocks left"
              value={
                status == null
                  ? "Connecting..."
                  : isHeaderSync
                    ? `Unknown. Syncing Headers (${status.headerHeight.toLocaleString()})...`
                    : blocksLeft > 0
                      ? blocksLeft.toLocaleString()
                      : "0"
              }
            />
            <Separator className="bg-(--color-btc-border)" />
            <StatRow
              label="Last block time"
              value={status ? formatBlockTime(status.lastBlockTime) : "Unknown"}
            />
            <Separator className="bg-(--color-btc-border)" />
            <StatRow
              label="Progress"
              value={
                <div className="flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:gap-3">
                  <span className="shrink-0 tabular-nums text-sm font-medium text-foreground">
                    {progressPct}%
                  </span>
                  <div className="h-3.5 min-w-0 flex-1 overflow-hidden rounded-md border border-(--color-btc-border) bg-(--color-btc-surface)">
                    <div
                      className="h-full rounded-sm bg-(--color-btc-gold) transition-[width] duration-500 ease-out"
                      style={{ width: `${progressWidth}%` }}
                    />
                  </div>
                </div>
              }
            />
            <Separator className="bg-(--color-btc-border)" />
            <StatRow
              label="Progress increase per hour"
              value={
                ratePerHour != null ? `${(ratePerHour * 100).toFixed(2)}%` : "calculating..."
              }
            />
            <Separator className="bg-(--color-btc-border)" />
            <StatRow label="Estimated time left until synced" value={eta} />
          </CardContent>
        </Card>
      </div>

      {/* Footer */}
      <div className="flex items-center justify-between gap-4 border-t border-(--color-btc-border) bg-(--color-btc-surface) px-6 py-3">
        <span className="font-mono text-xs text-(--color-btc-text-dim)">v{coinInfo.version}</span>
        <Button variant="outline" size="sm" onClick={onHide}>
          Hide
        </Button>
      </div>
    </div>
  );
}

function StatRow({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="grid gap-1 px-6 py-3 sm:grid-cols-[minmax(0,42%)_1fr] sm:gap-6 sm:py-3.5">
      <span className="text-sm font-medium text-foreground">{label}</span>
      <div className="text-sm text-(--color-btc-text-muted)">{value}</div>
    </div>
  );
}
