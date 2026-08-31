import { chromium } from 'playwright'
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirname = path.dirname(fileURLToPath(import.meta.url))
const appDir = path.resolve('C:\\Users\\haneg\\OneDrive - EDENRED\\Área de Trabalho\\projeto artigos\\app')
const outDir = path.join(appDir, 'test-results', 'ipad-evidence')
fs.mkdirSync(outDir, { recursive: true })

const VITE_PORT = 5173
const BASE = `http://127.0.0.1:${VITE_PORT}`

function wait(ms) { return new Promise(r => setTimeout(r, ms)) }

async function waitForServer(url, timeoutMs = 30000) {
  const start = Date.now()
  while (Date.now() - start < timeoutMs) {
    try {
      const r = await fetch(url, { signal: AbortSignal.timeout(2000) })
      if (r.ok || r.status < 500) return true
    } catch {}
    await wait(500)
  }
  throw new Error(`Server not ready at ${url}`)
}

async function main() {
  console.log('[capture] starting vite dev server...')
  const vite = spawn('npx', ['vite', '--host', '127.0.0.1', '--port', String(VITE_PORT)], {
    cwd: appDir,
    stdio: 'pipe',
    shell: true,
  })
  vite.stdout.on('data', d => process.stdout.write(`[vite] ${d}`))
  vite.stderr.on('data', d => process.stderr.write(`[vite:err] ${d}`))

  // ensure we kill vite on exit
  const cleanup = () => {
    try { vite.kill() } catch {}
  }
  process.on('exit', cleanup)
  process.on('SIGINT', () => { cleanup(); process.exit(1) })

  try {
    await waitForServer(`${BASE}/__ipad-preview`, 30000)
    console.log('[capture] vite ready, launching chromium...')

    const browser = await chromium.launch()

    const devices = [
      { name: 'ipad-mini-portrait', viewport: { width: 768, height: 1024 }, dpr: 2, isMobile: true, hasTouch: true, label: 'iPad Mini 768x1024 portrait @2x' },
      { name: 'ipad-mini-landscape', viewport: { width: 1024, height: 768 }, dpr: 2, isMobile: true, hasTouch: true, label: 'iPad Mini 1024x768 landscape @2x' },
      { name: 'ipad-pro11-portrait', viewport: { width: 834, height: 1194 }, dpr: 2, isMobile: true, hasTouch: true, label: 'iPad Pro 11 834x1194 portrait @2x' },
      { name: 'ipad-pro11-landscape', viewport: { width: 1194, height: 834 }, dpr: 2, isMobile: true, hasTouch: true, label: 'iPad Pro 11 1194x834 landscape @2x' },
    ]

    const ua = 'Mozilla/5.0 (iPad; CPU OS 12_2 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.5 Mobile/15E148 Safari/604.1'

    // 1) Preview shell screenshots (responsive, shows both frames)
    for (const d of devices.slice(0,2)) {
      const ctx = await browser.newContext({
        viewport: d.viewport,
        deviceScaleFactor: d.dpr,
        isMobile: d.isMobile,
        hasTouch: d.hasTouch,
        userAgent: ua,
      })
      const page = await ctx.newPage()
      await page.goto(`${BASE}/__ipad-preview`, { waitUntil: 'domcontentloaded', timeout: 15000 })
      await page.waitForTimeout(1500)
      const file = path.join(outDir, `preview-shell-${d.name}.png`)
      await page.screenshot({ path: file, fullPage: true })
      console.log(`[capture] saved ${file} (${d.viewport.width}x${d.viewport.height})`)
      await ctx.close()
    }

    // 2) Direct app viewport screenshots at iPad sizes (simulates real device without frame)
    // These verify GrokBanner/painel not clipped
    for (const d of devices) {
      const ctx = await browser.newContext({
        viewport: d.viewport,
        deviceScaleFactor: d.dpr,
        isMobile: d.isMobile,
        hasTouch: d.hasTouch,
        userAgent: ua,
      })
      const page = await ctx.newPage()
      // load app root with forced iPad frame param to ensure data-device=ipad
      await page.goto(`${BASE}/?__ipadFrame=1`, { waitUntil: 'domcontentloaded', timeout: 15000 })
      await page.waitForTimeout(1200)
      // wait for header/home
      try { await page.waitForSelector('.header, .biblioteca, .home', { timeout: 5000 }) } catch {}
      const file = path.join(outDir, `app-direct-${d.name}.png`)
      await page.screenshot({ path: file, fullPage: true })
      // also check for overflow and painel clipping evidence
      const info = await page.evaluate(() => {
        const painel = document.querySelector('.painel')
        const header = document.querySelector('.header')
        const grok = document.querySelector('.grok-banner')
        return {
          innerWidth: window.innerWidth,
          innerHeight: window.innerHeight,
          painelPosition: painel ? getComputedStyle(painel).position : null,
          painelDisplay: painel ? getComputedStyle(painel).display : null,
          headerHeight: header ? header.getBoundingClientRect().height : 0,
          grokVisible: !!grok,
          overflowX: document.documentElement.scrollWidth - window.innerWidth,
        }
      })
      console.log(`[capture] app-direct ${d.name}:`, JSON.stringify(info))
      fs.writeFileSync(path.join(outDir, `meta-${d.name}.json`), JSON.stringify(info, null, 2))
      await ctx.close()
    }

    // 3) Special preview shell full grid screenshot at desktop viewport (to show both frames together)
    {
      const ctx = await browser.newContext({ viewport: { width: 1600, height: 900 } })
      const page = await ctx.newPage()
      await page.goto(`${BASE}/__ipad-preview`, { waitUntil: 'domcontentloaded' })
      await page.waitForTimeout(1200)
      const file = path.join(outDir, `preview-grid-desktop.png`)
      await page.screenshot({ path: file, fullPage: true })
      console.log(`[capture] saved ${file}`)
      await ctx.close()
    }

    await browser.close()
    console.log('[capture] done, evidence in', outDir)
  } finally {
    cleanup()
    // give vite time to exit gracefully
    await wait(1000)
    try { vite.kill('SIGTERM') } catch {}
  }
}

main().catch(e => { console.error(e); process.exit(1) })
