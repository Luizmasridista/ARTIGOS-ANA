import { _electron as electron } from 'playwright'
import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirname = path.dirname(fileURLToPath(import.meta.url))
const appDir = path.resolve(dirname, '..')
const backendDir = path.resolve(appDir, '..', 'backend')
const API = 'http://127.0.0.1:8736'
const E2E_DATA = path.join(os.tmpdir(), 'ana-resp-data')
const E2E_PROFILE = path.join(os.tmpdir(), 'ana-resp-profile')

const wait = (ms) => new Promise((r) => setTimeout(r, ms))

async function healthOk() {
  try {
    const r = await fetch(`${API}/api/health`, { signal: AbortSignal.timeout(2000) })
    return r.ok
  } catch {
    return false
  }
}

async function subirBackend() {
  const exe = path.join(backendDir, 'bin', 'artigos-ana.exe')
  const out = fs.openSync(path.join(os.tmpdir(), 'ana-resp.log'), 'a')
  const p = spawn(exe, ['-port', '8736', '-data', E2E_DATA, '-www', ''], {
    cwd: backendDir,
    windowsHide: true,
    stdio: ['ignore', out, out],
  })
  p.unref()
}

async function garantirBackend() {
  for (let tentativa = 0; tentativa < 6; tentativa++) {
    if (await healthOk()) return
    subirBackend()
    for (let i = 0; i < 100 && !(await healthOk()); i++) await wait(300)
  }
  throw new Error('backend responsivo não subiu após 6 tentativas')
}

async function uploadPDF(titulo) {
  const pdfPath = path.join(backendDir, 'testdata', 'artigo-teste.pdf')
  const form = new FormData()
  form.append('file', new Blob([fs.readFileSync(pdfPath)], { type: 'application/pdf' }), 'artigo-teste.pdf')
  form.append('titulo', titulo)
  const res = await fetch(`${API}/api/artigos`, { method: 'POST', body: form })
  const body = await res.json()
  assert.equal(res.status, 201, `upload falhou: ${JSON.stringify(body)}`)
  return body
}

function linhasDasPalavras(palavras) {
  const linhas = []
  for (const w of palavras) {
    const centro = (w.y0 + w.y1) / 2
    const ultima = linhas[linhas.length - 1]
    if (ultima && Math.abs(centro - ultima.centro) < (w.y1 - w.y0) * 0.6) {
      ultima.palavras.push(w)
    } else {
      linhas.push({ centro, palavras: [w] })
    }
  }
  return linhas
}

function segmentoIntersectaCaixa(ax, ay, bx, by, x0, y0, x1, y1, tol) {
  const dx = bx - ax
  const dy = by - ay
  let t0 = 0
  let t1 = 1
  if (Math.abs(dx) < 1e-9) {
    if (ax < x0 - tol || ax > x1 + tol) return false
  } else {
    let ta = (x0 - tol - ax) / dx
    let tb = (x1 + tol - ax) / dx
    if (ta > tb) [ta, tb] = [tb, ta]
    t0 = Math.max(t0, ta)
    t1 = Math.min(t1, tb)
  }
  if (t0 > t1) return false
  if (Math.abs(dy) < 1e-9) {
    if (ay < y0 - tol || ay > y1 + tol) return false
  } else {
    let ta = (y0 - tol - ay) / dy
    let tb = (y1 + tol - ay) / dy
    if (ta > tb) [ta, tb] = [tb, ta]
    t0 = Math.max(t0, ta)
    t1 = Math.min(t1, tb)
  }
  return t0 <= t1
}

function palavrasNoTraco(palavras, ax, ay, bx, by) {
  return palavras.filter((p) => segmentoIntersectaCaixa(ax, ay, bx, by, p.x0, p.y0, p.x1, p.y1, 2))
}

const IPAD_LANDSCAPE = { largura: 1024, altura: 768 }
const IPAD_PORTRAIT = { largura: 768, altura: 1024 }

const titulo = `Responsivo ${Date.now()}`

await garantirBackend()
const artigo = await uploadPDF(titulo)
const cam = await (await fetch(`${API}/api/artigos/${artigo.id}/paginas/1/camada`)).json()
assert.ok(cam.palavras.length >= 8, 'PDF de teste sem palavras suficientes')
const linhas = linhasDasPalavras(cam.palavras)
assert.ok(linhas.length >= 3, 'PDF de teste sem linhas suficientes')

let electronApp
try {
  electronApp = await electron.launch({
    args: ['.', `--user-data-dir=${E2E_PROFILE}`],
    cwd: appDir,
  })
  const window = await electronApp.firstWindow()
  await window.waitForSelector('text=Artigos Ana', { timeout: 20000 })
  await window.evaluate(() => localStorage.setItem('artigosAnaApiBase', 'http://127.0.0.1:8736'))
  await window.reload()
  await window.waitForSelector('.biblioteca', { timeout: 20000 })
  await window.locator('.artigo-card', { hasText: titulo }).first().click()
  await window.waitForSelector('.pagina-camada', { timeout: 20000 })
  await wait(1000)

  async function redimensionar(largura, altura) {
    await electronApp.evaluate(
      ({ BrowserWindow }, { largura: w, altura: h }) => {
        const janela = BrowserWindow.getAllWindows()[0]
        janela.setSize(w, h)
      },
      { largura, altura },
    )
    await wait(900)
  }

  async function validarLayout(window, nome, largura) {
    const info = await window.evaluate(() => {
      const painel = document.querySelector('.painel')
      const btn = document.querySelector('.btn-painel')
      const header = document.querySelector('.header')
      const botoes = Array.from(
        document.querySelectorAll('.leitor-toolbar button, .painel-tabs button, .header button:not(.janela-btn)'),
      )
      const cs = (el) => (el ? getComputedStyle(el) : null)
      return {
        innerWidth: window.innerWidth,
        painelPosition: cs(painel)?.position,
        btnPainelDisplay: cs(btn)?.display,
        headerHeight: header ? header.getBoundingClientRect().height : 0,
        overflowX: document.documentElement.scrollWidth - window.innerWidth,
        menorToque: botoes.length ? Math.min(...botoes.map((b) => b.getBoundingClientRect().height)) : 0,
      }
    })
    console.log(`[layout] ${nome}:`, JSON.stringify(info))
    assert.ok(info.innerWidth <= largura, `${nome}: largura da janela acima do esperado`)
    assert.equal(info.painelPosition, 'fixed', `${nome}: painel deveria virar gaveta fixa`)
    assert.notEqual(info.btnPainelDisplay, 'none', `${nome}: botão de abrir/fechar painel deveria aparecer`)
    assert.ok(info.menorToque >= 40, `${nome}: alvo de toque menor que 40px`)
    assert.ok(info.overflowX <= 1, `${nome}: overflow horizontal detectado`)
  }

  async function alternarPainelPorToque(window) {
    const antes = await window.evaluate(() => document.querySelector('.painel')?.classList.contains('fechado'))
    await window.evaluate(() => {
      const b = document.querySelector('.btn-painel')
      const opts = { pointerType: 'touch', pointerId: 7, bubbles: true, cancelable: true }
      b.dispatchEvent(new PointerEvent('pointerdown', opts))
      b.dispatchEvent(new PointerEvent('pointerup', opts))
      b.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    })
    await wait(500)
    const depois = await window.evaluate(() => document.querySelector('.painel')?.classList.contains('fechado'))
    assert.notEqual(antes, depois, 'tocar no botão deveria alternar a gaveta do painel')
  }

  async function marcarComToque(window, ini, fim, indiceCor = 0) {
    const overlay = await window.locator('.pagina-camada').first().boundingBox()
    assert.ok(overlay, 'overlay sem caixa')
    const scale = overlay.width / cam.largura
    const centro = (w) => ({
      x: overlay.x + ((w.x0 + w.x1) / 2) * scale,
      y: overlay.y + ((w.y0 + w.y1) / 2) * scale,
    })
    const a = centro(ini)
    const b = centro(fim)
    const scrollAntes = await window.evaluate(() => document.querySelector('.leitor-paginas')?.scrollTop ?? 0)
    const passos = 12

    await window.evaluate(
      ({ a, b, passos }) => {
        for (let i = 0; i <= passos; i++) {
          const t = i / passos
          const x = a.x + (b.x - a.x) * t
          const y = a.y + (b.y - a.y) * t
          const el = document.elementFromPoint(x, y) || document.querySelector('.pagina-camada')
          if (el) {
            el.dispatchEvent(new PointerEvent(i === 0 ? 'pointerdown' : 'pointermove', {
              clientX: x, clientY: y, pointerType: 'touch', pointerId: 1, bubbles: true, cancelable: true,
            }))
          }
        }
      },
      { a, b, passos },
    )
    await window.evaluate(() => {
      window.dispatchEvent(new PointerEvent('pointerup', { pointerType: 'touch', pointerId: 1, bubbles: true }))
    })

    await window.locator('.tooltip-selecao').waitFor({ state: 'visible', timeout: 5000 })
    assert.ok((await window.locator('.sel-preview').count()) >= 1, 'tinta ao vivo deveria aparecer no toque')

    const scrollDepois = await window.evaluate(() => document.querySelector('.leitor-paginas')?.scrollTop ?? 0)
    assert.equal(scrollDepois, scrollAntes, 'arrastar para marcar não deveria rolar a página')

    const cor = await window.locator('.tooltip-cor').nth(indiceCor).boundingBox()
    await window.evaluate(
      ({ x, y }) => {
        const el = document.elementFromPoint(x, y) || document.querySelector('.tooltip-cor')
        if (el) {
          const opts = { clientX: x, clientY: y, pointerType: 'touch', pointerId: 2, bubbles: true, cancelable: true }
          el.dispatchEvent(new PointerEvent('pointerdown', opts))
          el.dispatchEvent(new PointerEvent('pointerup', opts))
          el.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
        }
      },
      { x: cor.x + cor.width / 2, y: cor.y + cor.height / 2 },
    )
    await window.locator('.destaque-ret').first().waitFor({ state: 'visible', timeout: 5000 })
    await wait(300)

    const ca = { x: (a.x - overlay.x) / scale, y: (a.y - overlay.y) / scale }
    const cb = { x: (b.x - overlay.x) / scale, y: (b.y - overlay.y) / scale }
    const esperadas = palavrasNoTraco(cam.palavras, ca.x, ca.y, cb.x, cb.y).map((w) => [w.x0, w.y0, w.x1, w.y1])
    return esperadas
  }

  const marcacoes = async () => await (await fetch(`${API}/api/artigos/${artigo.id}/marcacoes`)).json()

  // ---- iPad deitado (landscape) ----
  await redimensionar(IPAD_LANDSCAPE.largura, IPAD_LANDSCAPE.altura)
  await validarLayout(window, 'iPad landscape', 1024)

  await alternarPainelPorToque(window)
  await alternarPainelPorToque(window)

  const alvoL = linhas[0].palavras.slice(0, 3)
  const esperadasL = await marcarComToque(window, alvoL[0], alvoL[2], 0)
  let marcas = await marcacoes()
  assert.equal(marcas.length, 1, 'esperado 1 destaque criado por toque (landscape)')
  assert.deepEqual(marcas[0].palavras, esperadasL, 'o toque não cobriu o traço (landscape)')

  // ---- iPad em pé (portrait) ----
  await redimensionar(IPAD_PORTRAIT.largura, IPAD_PORTRAIT.altura)
  await validarLayout(window, 'iPad portrait', 768)

  const alvoP = linhas[1].palavras.slice(0, 3)
  const esperadasP = await marcarComToque(window, alvoP[0], alvoP[2], 1)
  marcas = await marcacoes()
  assert.equal(marcas.length, 2, 'esperado 2 destaques criados por toque (portrait)')
  assert.deepEqual(marcas[1].palavras, esperadasP, 'o toque não cobriu o traço (portrait)')

  console.log('RESPONSIVO OK: iPad landscape e portrait com gaveta, toque >= 40px e marcação por touchscreen')
} finally {
  if (electronApp) await electronApp.close()
}
