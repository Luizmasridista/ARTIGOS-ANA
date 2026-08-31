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
const REAL = !!process.env.E2E_REAL
const API = REAL ? 'http://127.0.0.1:8734' : 'http://127.0.0.1:8735'
const E2E_DATA = path.join(os.tmpdir(), 'ana-e2e-data')
const E2E_PROFILE = path.join(os.tmpdir(), 'ana-e2e-profile')

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
  const out = fs.openSync(path.join(os.tmpdir(), 'ana-e2e.log'), 'a')
  const args = REAL
    ? ['-port', '8734']
    : ['-port', '8735', '-data', E2E_DATA, '-www', '']
  const p = spawn(exe, args, {
    cwd: backendDir,
    windowsHide: true,
    stdio: ['ignore', out, out],
  })
  p.on('exit', (code, sig) => {
    fs.appendFileSync(path.join(os.tmpdir(), 'ana-e2e.log'), `[e2e] backend saiu code=${code} sig=${sig}\n`)
  })
  p.unref()
}

async function garantirBackend() {
  for (let tentativa = 0; tentativa < 6; tentativa++) {
    if (await healthOk()) return
    subirBackend()
    for (let i = 0; i < 100 && !(await healthOk()); i++) await wait(300)
  }
  throw new Error('backend e2e não subiu após 6 tentativas')
}

async function uploadPDF(titulo) {
  for (let tentativa = 0; tentativa < 3; tentativa++) {
    const pdfPath = path.join(backendDir, 'testdata', 'artigo-teste.pdf')
    const form = new FormData()
    form.append('file', new Blob([fs.readFileSync(pdfPath)], { type: 'application/pdf' }), 'artigo-teste.pdf')
    form.append('titulo', titulo)
    try {
      const res = await fetch(`${API}/api/artigos`, { method: 'POST', body: form })
      const body = await res.json()
      if (res.status === 201) return body
      throw new Error(`upload status ${res.status}: ${JSON.stringify(body)}`)
    } catch {
      await garantirBackend()
    }
  }
  throw new Error('upload falhou após 3 tentativas')
}

async function camada(artigoId, pagina = 1) {
  const res = await fetch(`${API}/api/artigos/${artigoId}/paginas/${pagina}/camada`)
  return res.json()
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

const titulo = `E2E Marcação ${Date.now()}`

await garantirBackend()
const artigo = await uploadPDF(titulo)
const cam = await camada(artigo.id)
assert.ok(cam.palavras.length >= 8, 'PDF de teste deveria ter várias palavras na página 1')
const palavras = cam.palavras
const linhas = linhasDasPalavras(palavras)
assert.ok(linhas.length >= 3, `esperado pelo menos 3 linhas, veio ${linhas.length}`)

let electronApp
try {
  electronApp = await electron.launch({
    args: REAL ? ['.'] : ['.', `--user-data-dir=${E2E_PROFILE}`],
    cwd: appDir,
  })
  const window = await electronApp.firstWindow()
  await window.waitForSelector('text=Artigos Ana', { timeout: 20000 })
  if (!REAL) {
    await window.evaluate(() => localStorage.setItem('artigosAnaApiBase', 'http://127.0.0.1:8735'))
    await window.reload()
  }
  await window.waitForSelector('.biblioteca', { timeout: 20000 })

  await window.locator('.artigo-card', { hasText: titulo }).first().waitFor({ timeout: 10000 })
  await window.locator('.artigo-card', { hasText: titulo }).first().click()

  await window.waitForSelector('.pagina-camada', { timeout: 20000 })
  await wait(800)

  const overlay = window.locator('.pagina-camada').first()
  const overlayBox = await overlay.boundingBox()
  assert.ok(overlayBox, 'overlay sem bounding box')
  const scale = overlayBox.width / cam.largura

  const centroPalavra = (w) => ({
    x: overlayBox.x + ((w.x0 + w.x1) / 2) * scale,
    y: overlayBox.y + ((w.y0 + w.y1) / 2) * scale,
  })

  async function arrastarEHighlightar(ini, fim, indiceCor) {
    const a = centroPalavra(ini)
    const b = centroPalavra(fim)
    await window.mouse.move(a.x, a.y)
    await window.mouse.down()
    await window.mouse.move(b.x, b.y, { steps: 15 })
    await window.mouse.up()
    const tooltip = window.locator('.tooltip-selecao')
    try {
      await tooltip.waitFor({ state: 'visible', timeout: 4000 })
    } catch {      const info = await window.evaluate(
        ({ ax, ay, bx, by }) => {
          const elA = document.elementFromPoint(ax, ay)
          const elB = document.elementFromPoint(bx, by)
          const sel = window.getSelection()
          return {
            selTexto: String(sel),
            rangeCount: sel?.rangeCount ?? -1,
            elA: elA ? `${elA.tagName}.${elA.className}` : null,
            elB: elB ? `${elB.tagName}.${elB.className}` : null,
            destaqueNoA: !!elA?.closest?.('.destaque-ret'),
            destaqueNoB: !!elB?.closest?.('.destaque-ret'),
          }
        },
        { ax: a.x, ay: a.y, bx: b.x, by: b.y },
      )
      const spans = await window.locator('.palavra').count()
      const destaques = await window.locator('.destaque-ret').count()
      console.error('DEBUG arrastar:', { ini: ini.texto, fim: fim.texto, a, b, overlayBox, scale, spans, destaques, ...info })
      throw new Error(`tooltip de seleção não apareceu (de ${ini.texto} até ${fim.texto})`)
    }
    const preview = window.locator('.sel-preview')
    assert.ok((await preview.count()) >= 1, 'a tinta em live mode deveria estar visível antes de escolher a cor')

    const cores = window.locator('.tooltip-cor')
    await cores.nth(indiceCor).click()
    await window.locator('.destaque-ret').nth(0).waitFor({ state: 'visible', timeout: 5000 })
    await wait(300)
  }

  const marcacoesAtuais = async () => (await (await fetch(`${API}/api/artigos/${artigo.id}/marcacoes`)).json())

  // Cenário 1: uma linha só
  {
    const alvo = linhas[0].palavras.slice(0, 3)
    await arrastarEHighlightar(alvo[0], alvo[alvo.length - 1], 0)
    await window.locator('.popover-destaque').waitFor({ state: 'visible', timeout: 4000 })
    const marcacoes = await marcacoesAtuais()
    assert.equal(marcacoes.length, 1, `esperado 1 destaque, veio ${marcacoes.length}`)
    const esperadas = alvo.map((w) => [w.x0, w.y0, w.x1, w.y1])
    assert.deepEqual(marcacoes[0].palavras, esperadas, 'palavras salvas diferem das selecionadas (linha única)')
  }

  // Cenário 2: arrasto por QUATRO linhas (quebra de linha) — a tinta segue o traço do cursor
  {
    const ini = linhas[0].palavras[linhas[0].palavras.length - 1]
    const fim = linhas[3].palavras[0]
    const a = centroPalavra(ini)
    const b = centroPalavra(fim)
    await arrastarEHighlightar(ini, fim, 1)

    const paraCamada = (p) => ({ x: (p.x - overlayBox.x) / scale, y: (p.y - overlayBox.y) / scale })
    const ca = paraCamada(a)
    const cb = paraCamada(b)
    const palavrasEsperadas = palavrasNoTraco(cam.palavras, ca.x, ca.y, cb.x, cb.y)
    const esperadas = palavrasEsperadas.map((w) => [w.x0, w.y0, w.x1, w.y1])

    const marcacoes = await marcacoesAtuais()
    assert.equal(marcacoes.length, 2, `esperado 2 destaques, veio ${marcacoes.length}`)
    assert.deepEqual(
      marcacoes[1].palavras,
      esperadas,
      'a tinta não cobriu exatamente o traço do cursor',
    )

    const foraDoTraco = cam.palavras.filter(
      (w) =>
        !palavrasEsperadas.includes(w) &&
        (w.y0 + w.y1) / 2 >= Math.min(ca.y, cb.y) &&
        (w.y0 + w.y1) / 2 <= Math.max(ca.y, cb.y),
    )
    for (const w of foraDoTraco) {
      assert.ok(
        !marcacoes[1].palavras.some((b) => b[0] === w.x0 && b[1] === w.y0),
        `palavra "${w.texto}" está no intervalo vertical do traço mas fora do caminho do cursor`,
      )
    }

    const destaque = window.locator('.destaque-ret')
    const linhasEsperadas = linhasDasPalavras(palavrasEsperadas)
    const segmentosCenario1 = 1
    assert.equal(
      await destaque.count(),
      segmentosCenario1 + linhasEsperadas.length,
      `esperado ${segmentosCenario1 + linhasEsperadas.length} segmentos no total (1 do cenário 1 + ${linhasEsperadas.length} do cenário 2)`,
    )
    const rects = []
    for (let i = 0; i < linhasEsperadas.length; i++) rects.push(await destaque.nth(segmentosCenario1 + i).boundingBox())
    rects.sort((r1, r2) => r1.y - r2.y)
    for (let i = 0; i < linhasEsperadas.length; i++) {
      const palavrasLinha = linhasEsperadas[i].palavras
      const esperado = {
        x: overlayBox.x + Math.min(...palavrasLinha.map((w) => w.x0)) * scale,
        y: overlayBox.y + Math.min(...palavrasLinha.map((w) => w.y0)) * scale,
        w: (Math.max(...palavrasLinha.map((w) => w.x1)) - Math.min(...palavrasLinha.map((w) => w.x0))) * scale,
        h: (Math.max(...palavrasLinha.map((w) => w.y1)) - Math.min(...palavrasLinha.map((w) => w.y0))) * scale,
      }
      for (const [campo, alvoV, atualV] of [
        ['x', esperado.x, rects[i].x],
        ['y', esperado.y, rects[i].y],
        ['largura', esperado.w, rects[i].width],
        ['altura', esperado.h, rects[i].height],
      ]) {
        assert.ok(
          Math.abs(alvoV - atualV) <= 3,
          `segmento ${i + 1} fora do lugar (${campo}): esperado ${alvoV.toFixed(1)} vs ${atualV.toFixed(1)}`,
        )
      }
    }
  }

  // Cenário 3: caneta de destaque — trocar cor e anotar a partir do bloco marcado
  {
    await window.locator('.destaque-ret').first().click()
    const popover = window.locator('.popover-destaque')
    await popover.waitFor({ state: 'visible', timeout: 4000 })
    assert.ok((await popover.locator('.popover-titulo').textContent())?.includes('Caneta'), 'popover sem título de caneta')

    const cores = popover.locator('.popover-cor')
    assert.equal(await cores.count(), 4, 'caneta deveria ter 4 cores')
    assert.ok((await cores.nth(0).getAttribute('class'))?.includes('ativa'), 'cor atual não marcada como ativa')

    await cores.nth(1).click()
    await wait(600)
    assert.ok(await popover.isVisible(), 'a caneta deveria continuar aberta após trocar a cor')
    assert.ok(
      (await popover.locator('.popover-cor').nth(1).getAttribute('class'))?.includes('ativa'),
      'cor verde deveria ficar ativa na caneta',
    )
    const marcacoes = await marcacoesAtuais()
    const alvo = marcacoes.find((m) => m.palavras.length === 3)
    assert.ok(alvo, 'marcação do cenário 1 sumiu')
    assert.equal(alvo.cor, '#9EE6A8', 'cor não trocada para verde')

    await window.locator('.popover-acoes .btn-primary').click()

    const textarea = window.locator('.painel textarea.input')
    await textarea.waitFor({ state: 'visible', timeout: 4000 })
    const valor = await textarea.inputValue()
    assert.ok(valor.includes('Olá'), `anotação deveria vir pré-preenchida com o trecho marcado, veio: "${valor}"`)

    await window.locator('.painel-composer .btn-primary').click()
    await window.locator('.nota-card').first().waitFor({ state: 'visible', timeout: 5000 })

    const badge = window.locator('.nota-badge')
    await badge.first().waitFor({ state: 'visible', timeout: 5000 })
    assert.equal(await badge.count(), 1, 'esperado 1 selo numerado no destaque com nota vinculada')
    assert.equal((await badge.first().textContent())?.trim(), '1', 'selo deveria mostrar o número 1')

    await badge.first().click()
    await wait(600)
    const focada = await window.locator('.nota-card.focada').count()
    assert.equal(focada, 1, 'clicar no selo deveria focar a nota no painel')

    // Cenário 1 do usuário: selecionar texto e clicar direto em "Nota" (sem escolher cor)
    {
      const alvo = linhas[1].palavras.slice(0, 3)
      const a = centroPalavra(alvo[0])
      const b = centroPalavra(alvo[alvo.length - 1])
      await window.mouse.move(a.x, a.y)
      await window.mouse.down()
      await window.mouse.move(b.x, b.y, { steps: 10 })
      await window.mouse.up()
      await window.locator('.tooltip-selecao').waitFor({ state: 'visible', timeout: 4000 })
      await window.locator('.tooltip-acao').click()

      const textarea = window.locator('.painel textarea.input')
      await textarea.waitFor({ state: 'visible', timeout: 4000 })
      const valor = await textarea.inputValue()
      assert.ok(valor.trim().length > 0, 'a anotação direta deveria vir pré-preenchida com o trecho')

      await window.locator('.painel-composer .btn-primary').click()
      await wait(800)

      const marcacoes = await marcacoesAtuais()
      const nova = marcacoes.find((m) => m.palavras.length === 3 && m.cor === '#FFE03B')
      assert.ok(nova, 'clicar em Nota deveria criar a marcação com a cor padrão (amarelo)')

      const notas = await (await fetch(`${API}/api/artigos/${artigo.id}/notas`)).json()
      const vinculada = notas.find((n) => n.marcacao_id === nova.id)
      assert.ok(vinculada, 'a nota criada pelo atalho deveria ficar vinculada à marcação')

      const badges = window.locator('.nota-badge')
      assert.equal(await badges.count(), 2, 'esperado 2 selos numerados (nota 1 e nota 2)')
      const textos = await badges.allTextContents()
      assert.deepEqual(textos.map((t) => t.trim()).sort(), ['1', '2'], 'numeração dos selos deveria ser 1 e 2')
    }

    // descolorir: apagar a tinta da marcação multi-linhas (identificada pela quantidade de palavras)
    {
      const marcas = await marcacoesAtuais()
      const multi = marcas.find((m) => m.palavras.length > 3)
      assert.ok(multi, 'marcação multi-linhas não encontrada')
      const comoPalavras = (caixas) => caixas.map(([x0, y0, x1, y1]) => ({ x0, y0, x1, y1 }))
      const antes = marcas.filter((m) => m.id < multi.id)
      const idxSegmento = antes.reduce((acc, m) => acc + linhasDasPalavras(comoPalavras(m.palavras)).length, 0)
      await window.locator('.destaque-ret').nth(idxSegmento).click()
      await window.locator('.popover-destaque').waitFor({ state: 'visible', timeout: 4000 })
      await window.locator('.popover-acoes .btn-ghost').click()
      await window.locator('.popover-acoes .btn-danger').click()
      await wait(600)
      const restantes = await marcacoesAtuais()
      assert.ok(!restantes.some((m) => m.id === multi.id), 'a marcação multi-linhas deveria ter sido apagada')
      assert.equal(restantes.length, 2, 'esperado 2 destaques após apagar a tinta do multi-linhas')
    }
  }

  // Cenário 4: persistência — recarregar o app e as marcações/numeração continuam lá
  {
    await window.reload()
    await window.waitForSelector('.biblioteca', { timeout: 20000 })
    await window.locator('.artigo-card', { hasText: titulo }).first().waitFor({ timeout: 10000 })
    await window.locator('.artigo-card', { hasText: titulo }).first().click()
    await window.waitForSelector('.pagina-camada', { timeout: 20000 })
    await wait(1200)

    const marcacoes = await marcacoesAtuais()
    assert.equal(marcacoes.length, 2, 'após recarregar, os destaques deveriam continuar salvos')
    assert.ok(
      await window.locator('.destaque-ret').first().isVisible(),
      'o destaque deveria estar renderizado após recarregar',
    )

    const badge = window.locator('.nota-badge')
    assert.equal(await badge.count(), 2, 'após recarregar, os selos numerados deveriam continuar visíveis')
    const textos = await badge.allTextContents()
    assert.deepEqual(textos.map((t) => t.trim()).sort(), ['1', '2'], 'numeração dos selos errada após recarregar')

    const numerosCard = await window.locator('.nota-card-numero').allTextContents()
    assert.deepEqual(numerosCard.map((t) => t.trim()).sort(), ['1', '2'], 'numeração dos cartões errada após recarregar')
  }

  console.log('E2E OK: tinta ao vivo, traço multi-linhas, caneta (cor/anotar), selo da nota, apagar tinta e persistência')
} finally {
  if (electronApp) await electronApp.close()
}
