import { lazy, StrictMode, Suspense } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './styles.css'
import { isIpadUA } from './hooks/useIpad'
import { registerSW } from './registerSW'

const IpadLivePreview = lazy(() => import('./components/IpadLivePreview'))

function isPreviewRoute(): boolean {
  try {
    const url = new URL(window.location.href)
    if (url.pathname === '/__ipad-preview' || url.pathname === '/__ipad-preview/') return true
    if (url.searchParams.has('ipadPreview') || url.searchParams.has('__ipad-preview')) return true
    // ?ipadPreview=1 legacy query
    if (url.searchParams.get('ipadPreview') === '1') return true
  } catch {
    // ignore
  }
  return false
}

function isIpadFrame(): boolean {
  try {
    const sp = new URLSearchParams(window.location.search)
    return sp.has('__ipadFrame') || sp.has('__ipadEmu')
  } catch {
    return false
  }
}

const preview = isPreviewRoute()
const frameEmu = isIpadFrame()

if (!window.artigosAna) document.documentElement.dataset.modo = 'web'

// PWA: registra SW apenas em web (nao no Electron)
if (!window.artigosAna) {
  try {
    registerSW()
  } catch {
    // ignore
  }
}

// preview shell itself stays desktop (to layout frames); frames will simulate ipad via ?__ipadFrame=1
if (!preview) {
  try {
    if (!window.artigosAna && frameEmu) {
      document.documentElement.dataset.device = 'ipad'
    } else {
      const ua = navigator.userAgent || ''
      const maxTouch = (navigator as unknown as { maxTouchPoints?: number }).maxTouchPoints ?? 0
      const hasTouch = 'ontouchend' in window || maxTouch > 0
      if (!window.artigosAna && isIpadUA(ua, maxTouch, hasTouch)) {
        document.documentElement.dataset.device = 'ipad'
      }
    }
  } catch {
    // ignore
  }
}

const rootEl = document.getElementById('root')!

if (preview) {
  createRoot(rootEl).render(
    <StrictMode>
      <Suspense fallback={<div style={{ display: 'grid', placeItems: 'center', minHeight: '100dvh', color: 'var(--text-2)' }}>Carregando preview…</div>}>
        <IpadLivePreview />
      </Suspense>
    </StrictMode>,
  )
} else {
  createRoot(rootEl).render(
    <StrictMode>
      <App />
    </StrictMode>,
  )
}
