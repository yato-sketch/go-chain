import { useEffect, useState } from "react";
import { useCoinInfo } from "../hooks/useCoinInfo";
import {
  GetBalance,
  GetWalletAddress,
  GetBlockchainInfo,
} from "../../wailsjs/go/main/App";
import { Button } from "@/components/ui/button";

export function Overview() {
  const coinInfo = useCoinInfo();
  const [confirmed, setConfirmed] = useState(0);
  const [unconfirmed, setUnconfirmed] = useState(0);
  const [address, setAddress] = useState("");
  const [height, setHeight] = useState(0);
  const [bestHash, setBestHash] = useState("");

  useEffect(() => {
    const poll = () => {
      GetBalance().then((b) => {
        setConfirmed(b.confirmed as number);
        setUnconfirmed(b.unconfirmed as number);
      });
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setBestHash(info.bestHash as string);
      });
    };
    GetWalletAddress().then(setAddress);
    poll();
    const id = setInterval(poll, 3000);
    return () => clearInterval(id);
  }, []);

  return (
    <div className="space-y-6">
      {/* Balance card */}
      <div className="rounded-lg border border-gray-700 bg-gray-800 p-6">
        <h3 className="mb-1 text-sm font-medium text-gray-400">Balance</h3>
        <p className="text-3xl font-bold text-white">
          {confirmed.toFixed(coinInfo.decimals > 4 ? 4 : coinInfo.decimals)}{" "}
          <span className="text-lg text-gray-400">{coinInfo.ticker}</span>
        </p>
        {unconfirmed > 0 && (
          <p className="mt-1 text-sm text-yellow-400">
            +{unconfirmed.toFixed(4)} {coinInfo.ticker} unconfirmed
          </p>
        )}
      </div>

      {/* Receive address */}
      <div className="rounded-lg border border-gray-700 bg-gray-800 p-6">
        <h3 className="mb-2 text-sm font-medium text-gray-400">
          Default Address
        </h3>
        <code className="block break-all rounded bg-gray-900 px-3 py-2 text-sm text-green-400">
          {address || "Loading..."}
        </code>
      </div>

      {/* Chain info */}
      <div className="rounded-lg border border-gray-700 bg-gray-800 p-6">
        <h3 className="mb-3 text-sm font-medium text-gray-400">
          Chain Status
        </h3>
        <dl className="grid grid-cols-2 gap-4 text-sm">
          <div>
            <dt className="text-gray-500">Block Height</dt>
            <dd className="font-mono text-white">
              {height.toLocaleString()}
            </dd>
          </div>
          <div>
            <dt className="text-gray-500">Best Block</dt>
            <dd className="truncate font-mono text-white" title={bestHash}>
              {bestHash ? bestHash.slice(0, 16) + "..." : "—"}
            </dd>
          </div>
          <Button>Send</Button>
        </dl>
      </div>
    </div>
  );
}
