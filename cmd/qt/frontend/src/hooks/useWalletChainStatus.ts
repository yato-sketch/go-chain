import { useEffect, useState } from "react";
import { GetBlockchainInfo, GetPeerCount } from "../../wailsjs/go/main/App";

export function useWalletChainStatus(pollMs = 3000) {
  const [height, setHeight] = useState(0);
  const [network, setNetwork] = useState("");
  const [peers, setPeers] = useState(0);

  useEffect(() => {
    const poll = () => {
      GetBlockchainInfo().then((info) => {
        setHeight(info.height as number);
        setNetwork((info.network as string) || "");
      });
      GetPeerCount().then(setPeers);
    };
    poll();
    const id = setInterval(poll, pollMs);
    return () => clearInterval(id);
  }, [pollMs]);

  return { height, network, peers };
}
