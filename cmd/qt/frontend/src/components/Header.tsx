import { useEffect, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import {
  GetBlockchainInfo,
  GetPeerCount,
} from "../../wailsjs/go/main/App";

export function Header() {
  const coinInfo = useCoinInfo();
  const [height, setHeight] = useState(0);
  const [network, setNetwork] = useState("");
  const [peers, setPeers] = useState(0);

  useEffect(() => {
    const poll = () => {
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setNetwork(info.network as string);
      });
      GetPeerCount().then(setPeers);
    };
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, []);

  return (
    <header className="flex items-center justify-between border-b border-gray-700 bg-gray-800 px-6 py-3">
      <div className="flex items-center gap-4">
        <h2 className="text-sm font-semibold text-white">
          {coinInfo.name} Wallet
        </h2>
        {network && (
          <span className="rounded bg-blue-600/20 px-2 py-0.5 text-xs font-medium text-blue-400">
            {network}
          </span>
        )}
      </div>
      <div className="flex items-center gap-6 text-xs text-gray-400">
        <span>Block {height.toLocaleString()}</span>
        <span>{peers} peer{peers !== 1 ? "s" : ""}</span>
      </div>
    </header>
  );
}
