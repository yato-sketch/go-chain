import { useCoinInfo } from "../hooks/useCoinInfo";

type Page = "overview" | "send" | "receive" | "transactions" | "network" | "mining" | "console";

interface SidebarProps {
  currentPage: Page;
  onNavigate: (page: Page) => void;
}

const navItems: { id: Page; label: string; enabled: boolean }[] = [
  { id: "overview", label: "Overview", enabled: true },
  { id: "send", label: "Send", enabled: false },
  { id: "receive", label: "Receive", enabled: false },
  { id: "transactions", label: "Transactions", enabled: false },
  { id: "network", label: "Network", enabled: false },
  { id: "mining", label: "Mining", enabled: false },
  { id: "console", label: "Console", enabled: false },
];

export function Sidebar({ currentPage, onNavigate }: SidebarProps) {
  const coinInfo = useCoinInfo();

  return (
    <aside className="flex w-52 flex-col border-r border-gray-700 bg-gray-800">
      <div className="px-4 py-5">
        <h1 className="text-lg font-bold text-white">{coinInfo.name}</h1>
        <span className="text-xs text-gray-400">v{coinInfo.version}</span>
      </div>
      <nav className="flex-1 space-y-1 px-2">
        {navItems.map((item) => (
          <button
            key={item.id}
            onClick={() => item.enabled && onNavigate(item.id)}
            disabled={!item.enabled}
            className={`w-full rounded px-3 py-2 text-left text-sm transition-colors ${
              currentPage === item.id
                ? "bg-gray-700 text-white font-medium"
                : item.enabled
                ? "text-gray-300 hover:bg-gray-700/50 hover:text-white"
                : "text-gray-600 cursor-not-allowed"
            }`}
          >
            {item.label}
          </button>
        ))}
      </nav>
      <div className="border-t border-gray-700 px-4 py-3">
        <p className="text-xs text-gray-500">{coinInfo.copyright}</p>
      </div>
    </aside>
  );
}
