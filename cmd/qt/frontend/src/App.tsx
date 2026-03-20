import { useEffect, useState } from "react";
import { CoinInfo, CoinInfoContext } from "./hooks/useCoinInfo";
import { Sidebar } from "./components/Sidebar";
import { Header } from "./components/Header";
import { Overview } from "./pages/Overview";
import { CoinInfo as GetCoinInfo } from "../wailsjs/go/main/App";

type Page = "overview" | "send" | "receive" | "transactions" | "network" | "mining" | "console";

function App() {
  const [coinInfo, setCoinInfo] = useState<CoinInfo | null>(null);
  const [page, setPage] = useState<Page>("overview");

  useEffect(() => {
    GetCoinInfo().then((info) => setCoinInfo(info as unknown as CoinInfo));
  }, []);

  if (!coinInfo) {
    return (
      <div className="flex h-full items-center justify-center bg-gray-900 text-gray-400">
        Starting node...
      </div>
    );
  }

  return (
    <CoinInfoContext.Provider value={coinInfo}>
      <div className="flex h-full bg-gray-900">
        <Sidebar currentPage={page} onNavigate={setPage} />
        <div className="flex flex-1 flex-col overflow-hidden">
          <Header />
          <main className="flex-1 overflow-auto p-6">
            {page === "overview" && <Overview />}
            {page !== "overview" && (
              <div className="flex h-full items-center justify-center text-gray-500">
                Coming in Phase 2
              </div>
            )}
          </main>
        </div>
      </div>
    </CoinInfoContext.Provider>
  );
}

export default App;
