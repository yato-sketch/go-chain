import { useCoinInfo } from "../hooks/useCoinInfo";

export function Header() {
  const coinInfo = useCoinInfo();
  const showNetBadge = coinInfo.network && coinInfo.network !== "mainnet";

  return (
    <header
      className="btc-noise relative flex items-center px-6 py-3"
      style={{
        background: 'var(--color-btc-surface)',
        borderBottom: '1px solid var(--color-btc-border)',
      }}
    >
      <div className="relative z-10 flex items-center gap-4">
        <h2 className="text-sm font-semibold" style={{ color: 'var(--color-btc-text)' }}>
          {coinInfo.name} <span style={{ color: 'var(--color-btc-text-dim)' }} className="font-normal">Wallet</span>
          {showNetBadge && (
            <span
              style={{
                marginLeft: 8,
                padding: '1px 8px',
                borderRadius: 3,
                fontSize: '0.7rem',
                fontWeight: 600,
                letterSpacing: '0.04em',
                textTransform: 'uppercase',
                background: coinInfo.network === 'regtest' ? '#6b3a3a' : '#3a4a6b',
                color: coinInfo.network === 'regtest' ? '#f5a0a0' : '#a0c4f5',
                border: `1px solid ${coinInfo.network === 'regtest' ? '#8b4a4a' : '#4a5a7b'}`,
              }}
            >
              {coinInfo.network}
            </span>
          )}
        </h2>
      </div>
    </header>
  );
}
