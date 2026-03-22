import { useEffect, useState } from "react";
import { BrowserRouter } from "react-router-dom";
import { CoinInfoContext } from "./hooks/useCoinInfo";
import { TooltipProvider } from "@/components/ui/tooltip";
import { CoinInfo as GetCoinInfo } from "../wailsjs/go/main/App";
import { CoinInfo } from "./lib/types";
import AppRoutes from "./routes";
import MainLayout from "./components/layout/MainLayout";

function App() {
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);

  useEffect(() => {
    GetCoinInfo().then((info) => setCoinInfo(info as unknown as CoinInfo));
  }, []);

  if (!coinInfo) {
    return (
      <div className="flex h-full items-center justify-center" style={{ background: 'var(--color-btc-deep)' }}>
        <div className="flex flex-col items-center gap-4">
          <div className="h-10 w-10 animate-spin rounded-full border-2 border-transparent" style={{ borderTopColor: 'var(--color-btc-gold)' }} />
          <span style={{ color: 'var(--color-btc-text-muted)' }} className="text-sm">Starting node...</span>
        </div>
      </div>
    );
  }

  return (
    <BrowserRouter>
      <CoinInfoContext.Provider value={coinInfo}>
        <TooltipProvider>
          <MainLayout>
            <AppRoutes />
          </MainLayout>
        </TooltipProvider>
      </CoinInfoContext.Provider>
    </BrowserRouter>
  );
}

export default App;
