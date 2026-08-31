import { useState } from 'react'

const DEVICES = [
  { label: 'iPad Mini (768×1024)', width: 768, height: 1024, device: 'iPad Mini' },
  { label: 'iPad Pro 11 (834×1194)', width: 834, height: 1194, device: 'iPad Pro 11' },
  { label: 'iPad 10th (820×1180)', width: 820, height: 1180, device: 'iPad (gen 10)' },
] as const

export function IpadLivePreview() {
  const [orientation, setOrientation] = useState<'portrait' | 'landscape'>('portrait')
  const [scale, setScale] = useState(0.6)

  const base = typeof window !== 'undefined' ? window.location.origin : ''
  // mesma origem, sem param de preview para não recursar
  const src = base + '/'

  return (
    <div className="ipad-preview-page">
      <div className="ipad-preview-toolbar">
        <h1 className="ipad-preview-titulo">iPad Live Preview — debug ao vivo</h1>
        <span className="ipad-preview-dica">Fonte da verdade: Playwright devices['iPad Mini'/'iPad Pro 11'] — viewport + WebKit + touch</span>
        <div className="ipad-preview-controles">
          <button
            type="button"
            className={`btn btn-sm ${orientation === 'portrait' ? 'btn-primary' : ''}`}
            onClick={() => setOrientation('portrait')}
          >
            Retrato
          </button>
          <button
            type="button"
            className={`btn btn-sm ${orientation === 'landscape' ? 'btn-primary' : ''}`}
            onClick={() => setOrientation('landscape')}
          >
            Paisagem
          </button>
          <label className="ipad-preview-scale">
            Zoom
            <input
              type="range"
              min={0.4}
              max={1}
              step={0.05}
              value={scale}
              onChange={(e) => setScale(Number(e.target.value))}
            />
            <span>{Math.round(scale * 100)}%</span>
          </label>
          <a className="btn btn-sm" href="/" target="_blank" rel="noreferrer">
            Abrir app
          </a>
        </div>
        <p className="ipad-preview-ajuda">
          Best practice 2026: use <code>npx playwright test --ui --project="iPad Mini"</code> para emulação fiel (WebKit) + Safari Web Inspector via USB no iPad real para final. Este preview é para iteração rápida de CSS (safe-area, dvh, drawer).
        </p>
      </div>

      <div className="ipad-preview-grid">
        {DEVICES.map((d) => {
          const w = orientation === 'portrait' ? d.width : d.height
          const h = orientation === 'portrait' ? d.height : d.width
          return (
            <div key={d.label} className="ipad-frame-wrap">
              <div className="ipad-frame-label">
                {d.label} — {orientation} {w}×{h} <span className="ipad-frame-device">{d.device}</span>
              </div>
              <div
                className="ipad-frame"
                style={{
                  width: w,
                  height: h,
                  transform: `scale(${scale})`,
                  transformOrigin: 'top left',
                }}
              >
                <iframe
                  title={`${d.label} ${orientation}`}
                  src={src}
                  width={w}
                  height={h}
                  style={{ width: w, height: h, border: 0, background: 'white' }}
                  loading="lazy"
                />
              </div>
              <div className="ipad-frame-foot" style={{ width: w * scale }}>
                {w}×{h} @ {scale * 100 | 0}% — arraste vertical deve rolar, horizontal deve marcar
              </div>
            </div>
          )
        })}
      </div>
    </div>
  )
}

export default IpadLivePreview
