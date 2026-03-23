import { createContext, useContext } from "react";

export interface CoinInfo {
  name: string;
  nameLower: string;
  ticker: string;
  decimals: number;
  baseUnitName: string;
  displayUnitName: string;
  version: string;
  copyright: string;
  network: string;
}

export const CoinInfoContext = createContext<CoinInfo | null>(null);

export function useCoinInfo(): CoinInfo {
  const ctx = useContext(CoinInfoContext);
  if (!ctx) {
    throw new Error("useCoinInfo must be used within CoinInfoContext.Provider");
  }
  return ctx;
}
