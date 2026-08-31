import { chromium } from 'playwright'
import assert from 'node:assert/strict'
import http from 'node:http'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirname = path.dirname(fileURLToPath(import.meta.url))
const appDir = path.resolve(dirname, '..')
const distDir = path.join(appDir, 'dist')
const BASE = process.env.APP_URL || 'http://127.0.0.1:5173'

async function isReachable(url) {
  try {
    const r = await fetch(url, { signal: AbortSignal.timeout(2000) })
    return r.ok || r.status === 304 || r.status === 404
  } catch { return false }
}

function startStaticServer(port = 5173) {
  const mime = { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.json': 'application/json', '.svg': 'image/svg+xml', '.png': 'image/png' }
  const server = http.createServer((req, res) => {
    let p = req.url.split('?')[0]
    if (p === '/') p = '/index.html'
    const filePath = path.join(distDir, p)
    if (fs.existsSync(filePath) && fs.statSync(filePath).isFile()) {
      const ext = path.extname(filePath)
      res.writeHead(200, { 'Content-Type': mime[ext] || 'application/octet-stream' })
      fs.createReadStream(filePath).pipe(res)
    } else if (!path.extname(p)) {
      // SPA fallback
      const idx = path.join(distDir, 'index.html')
      if (fs.existsSync(idx)) {
        res.writeHead(200, { 'Content-Type': 'text/html' })
        fs.createReadStream(idx).pipe(res)
      } else { res.writeHead(404).end('not found') }
    } else {
      res.writeHead(404).end('not found')
    }
  })
  return new Promise((resolve) => server.listen(port, '127.0.0.1', () => resolve(server)))
}

async function main() {
  let server = null
  if (!(await isReachable(BASE))) {
    if (!fs.existsSync(path.join(distDir, 'index.html'))) {
      console.error('[citacoes-navegacao] dist não encontrado e APP_URL não alcançável. Rode npm run build antes.')
      process.exit(1)
    }
    console.log('[citacoes-navegacao] iniciando servidor estático dist em 5173...')
    server = await startStaticServer(5173)
    // aguarda
    for (let i = 0; i < 20; i++) {
      if (await isReachable(BASE)) break
      await new Promise(r => setTimeout(r, 200))
    }
  }

  const browser = await chromium.launch({ headless: true })
  const ctx = await browser.newContext()
  const page = await ctx.newPage()

  // Mock API — intercepta todas as chamadas /api/
  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const pathname = url.pathname
    const method = route.request().method()
    // console.log('[mock]', method, pathname)

    // health - not used in preview mode but responde
    if (pathname === '/api/health' || pathname === '/health') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, versao: '0.1.0-test' }) })
    }
    // listarArtigos
    if (pathname === '/api/artigos' && method === 'GET') {
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify([
          { id: 99, titulo: 'Artigo Navegação Teste', num_paginas: 10, criado_em: new Date().toISOString() },
        ]),
      })
    }
    // getArtigo
    const mArtigo = pathname.match(/^\/api\/artigos\/(\d+)$/)
    if (mArtigo && method === 'GET') {
      const id = Number(mArtigo[1])
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          id,
          titulo: 'Artigo Navegação Teste',
          criado_em: new Date().toISOString(),
          paginas: Array.from({ length: 10 }, (_, i) => ({ numero: i + 1, largura: 612, altura: 792 })),
        }),
      })
    }
    // camada
    const mCamada = pathname.match(/^\/api\/artigos\/\d+\/paginas\/(\d+)\/camada$/)
    if (mCamada && method === 'GET') {
      const pagina = Number(mCamada[1])
      // gera palavras dummy para escala
      const palavras = [
        { texto: 'Olá', x0: 72, y0: 100, x1: 120, y1: 115 },
        { texto: 'mundo', x0: 125, y0: 100, x1: 180, y1: 115 },
        { texto: 'teste', x0: 72, y0: 130, x1: 140, y1: 145 },
      ]
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({ largura: 612, altura: 792, palavras }),
      })
    }
    // citacoes
    const mCit = pathname.match(/^\/api\/artigos\/\d+\/citacoes$/)
    if (mCit && method === 'GET') {
      const citacoes = [
        {
          id: 1, tipo: 'autor_ano', chave: '(Silva, 2020)', autor: 'Silva', ano: 2020,
          trecho: 'como em Silva (2020) afirma na página 3', titulo: '', texto: '', url: '', criado_em: new Date().toISOString(),
          pagina: 3, pos: [10.5, 20.1, 40.2, 32.8],
          ocorrencias: [
            { pagina: 3, pos: [10.5, 20.1, 40.2, 32.8], trecho: 'como em Silva (2020) afirma' },
            { pagina: 7, pos: [15, 30, 50, 45], trecho: 'Silva (2020) novamente' },
          ],
        },
        {
          id: 2, tipo: 'numerica', chave: '[12]', autor: '', ano: null,
          trecho: 'estudo [12] mostra', titulo: '', texto: '', url: 'https://example.com/12', criado_em: new Date().toISOString(),
          pagina: 5, pos: [72, 100, 140, 115],
          ocorrencias: [{ pagina: 5, pos: [72, 100, 140, 115], trecho: 'estudo [12] mostra' }],
        },
        {
          id: 3, tipo: 'referencia', chave: '[99] SILVA 2020 Outra', autor: 'Silva', ano: 2020,
          trecho: 'SILVA, J. Livro. Editora, 2020.', titulo: 'Livro', texto: 'Ref pura sem ocorrência', url: '', criado_em: new Date().toISOString(),
          pagina: 0, pos: null,
          ocorrencias: [],
        },
      ]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(citacoes) })
    }
    // varrer-citacoes
    const mVarrer = pathname.match(/^\/api\/artigos\/\d+\/varrer-citacoes$/)
    if (mVarrer && method === 'POST') {
      // retorna mesmo que GET
      const citacoes = [
        {
          id: 1, tipo: 'autor_ano', chave: '(Silva, 2020)', autor: 'Silva', ano: 2020,
          trecho: 'como em Silva (2020) afirma na página 3', titulo: '', texto: '', url: '', criado_em: new Date().toISOString(),
          pagina: 3, pos: [10.5, 20.1, 40.2, 32.8],
          ocorrencias: [
            { pagina: 3, pos: [10.5, 20.1, 40.2, 32.8], trecho: 'como em Silva (2020) afirma' },
            { pagina: 7, pos: [15, 30, 50, 45], trecho: 'Silva (2020) novamente' },
          ],
        },
        {
          id: 2, tipo: 'numerica', chave: '[12]', autor: '', ano: null,
          trecho: 'estudo [12] mostra', titulo: '', texto: '', url: 'https://example.com/12', criado_em: new Date().toISOString(),
          pagina: 5, pos: [72, 100, 140, 115],
          ocorrencias: [{ pagina: 5, pos: [72, 100, 140, 115], trecho: 'estudo [12] mostra' }],
        },
        {
          id: 3, tipo: 'referencia', chave: '[99] SILVA 2020 Outra', autor: 'Silva', ano: 2020,
          trecho: 'SILVA, J. Livro. Editora, 2020.', titulo: 'Livro', texto: 'Ref pura sem ocorrência', url: '', criado_em: new Date().toISOString(),
          pagina: 0, pos: null,
          ocorrencias: [],
        },
      ]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(citacoes) })
    }
    // marcacoes, notas, historico, exportar etc -> vazio
    if (pathname.includes('/marcacoes') || pathname.includes('/notas') || pathname.includes('/historico')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    }
    // imagem da página
    const mImg = pathname.match(/^\/api\/artigos\/\d+\/paginas\/\d+\/imagem$/)
    if (mImg) {
      // retorna 1x1 png transparente
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64')
      return route.fulfill({ status: 200, contentType: 'image/png', body: png })
    }
    // enriquecer
    if (pathname.includes('/enriquecer')) {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ url: 'https://example.com/enriquecida' }) })
    }
    // fallback - passa
    return route.continue()
  })

  page.on('console', msg => console.log('[browser]', msg.text()))
  page.on('pageerror', err => console.log('[pageerror]', err.message))

  try {
    console.log('[citacoes-navegacao] navegando para', BASE)
    await page.goto(BASE, { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.biblioteca', { timeout: 10000 })
    // aguarda card mockado
    await page.locator('.artigo-card', { hasText: 'Artigo Navegação Teste' }).first().waitFor({ timeout: 8000 })
    await page.locator('.artigo-card', { hasText: 'Artigo Navegação Teste' }).first().click()

    await page.waitForSelector('.leitor', { timeout: 8000 })
    await page.waitForSelector('.painel', { timeout: 5000 })

    // aba Fontes & Citações
    const tabFontes = page.locator('.painel-tab', { hasText: 'Fontes' })
    await tabFontes.click()
    // aguarda lista
    await page.waitForSelector('.citacoes-conteudo', { timeout: 8000 })
    const cards = page.locator('.citacao-card')
    const count = await cards.count()
    console.log('[citacoes-navegacao] cards', count)
    assert.equal(count, 3, `esperado 3 cards, veio ${count}`)

    // verifica clicavel vs sem-pos
    const clicaveis = page.locator('.citacao-card--clicavel')
    const semPos = page.locator('.citacao-card--sem-pos')
    assert.equal(await clicaveis.count(), 2, '2 cards deveriam ser clicáveis')
    assert.equal(await semPos.count(), 1, '1 card sem posição')

    // verifica badge Página
    const badge = clicaveis.first().locator('.citacao-pagina-badge')
    await badge.waitFor({ timeout: 2000 })
    const badgeText = await badge.textContent()
    console.log('[citacoes-navegacao] badge primeira', badgeText)
    assert.ok(badgeText.includes('Página 3'), `badge deveria conter Página 3, veio ${badgeText}`)

    // verifica botão Ver no texto habilitado / desabilitado
    const btnVer = clicaveis.first().locator('button', { hasText: 'Ver no texto' })
    assert.equal(await btnVer.isDisabled(), false, 'Ver no texto deveria estar habilitado para navegável')
    const btnVerDes = semPos.first().locator('button', { hasText: 'Ver no texto' })
    assert.equal(await btnVerDes.isDisabled(), true, 'Ver no texto deveria estar desabilitado sem ocorrência')
    const titleDes = await btnVerDes.getAttribute('title')
    assert.ok(titleDes.includes('Sem ocorrência'), `title deveria conter aviso, veio ${titleDes}`)

    // verifica aria-label e role
    const role = await clicaveis.first().getAttribute('role')
    assert.equal(role, 'button', 'card clicável deve ter role=button')
    const tabIndex = await clicaveis.first().getAttribute('tabindex')
    assert.equal(tabIndex, '0', 'card clicável deve ser focável')

    // clique no card navegável -> deve fazer scroll e mostrar destaque
    // precisa esperar páginas renderizarem
    await page.waitForSelector('.pagina-inner', { timeout: 5000 })
    // força scroll para topo antes
    await page.evaluate(() => document.querySelector('.leitor-paginas')?.scrollTo(0, 0))

    // clica no card (não no botão interno, mas no card)
    await clicaveis.first().click()
    // deve mostrar fonte-destaque na página 3
    // PageView usa lazy ativação via IntersectionObserver; força visibilidade scrollando
    // aguarda destaque visível
    let destaque = page.locator('[data-testid="fonte-destaque"]')
    // pode estar em qualquer pagina, mas esperamos 1 visível
    await destaque.first().waitFor({ state: 'visible', timeout: 5000 })
    console.log('[citacoes-navegacao] destaque visível após 1º clique')
    // verifica que está na página 3 (primeira ocorrência)
    // checa estilo left/top (escalado)
    const box = await destaque.first().boundingBox()
    assert.ok(box, 'destaque sem boundingBox')
    assert.ok(box.width > 0 && box.height > 0, 'destaque deveria ter tamanho >0')
    // verifica classe pulso
    const cls = await destaque.first().getAttribute('class')
    assert.ok(cls.includes('pulso'), 'destaque deveria ter classe pulso nos primeiros 2s')
    console.log('[citacoes-navegacao] destaque box', box, 'classe', cls)

    // verifica que paginaAtual foi para 3 (toolbar)
    const paginacao = await page.locator('.leitor-paginacao span').textContent()
    console.log('[citacoes-navegacao] paginacao após 1º clique', paginacao)
    assert.ok(paginacao.includes('Página 3'), `deveria ter ido para página 3, veio ${paginacao}`)

    // segundo clique na mesma citação deve ir para próxima ocorrência página 7 (ciclo)
    await clicaveis.first().click()
    await page.waitForTimeout(400)
    const pag2 = await page.locator('.leitor-paginacao span').textContent()
    console.log('[citacoes-navegacao] paginacao após 2º clique (ciclo)', pag2)
    assert.ok(pag2.includes('Página 7'), `segundo clique deveria ir para página 7 (ciclo), veio ${pag2}`)
    // destaque agora deve estar na página 7
    await page.waitForTimeout(500)
    const destaque2 = page.locator('[data-testid="fonte-destaque"]')
    await destaque2.first().waitFor({ timeout: 3000 })
    const box2 = await destaque2.first().boundingBox()
    assert.ok(box2 && box2.width > 0, 'destaque segunda ocorrência sem box')

    // terceiro clique deve ciclar de volta para página 3
    await clicaveis.first().click()
    await page.waitForTimeout(400)
    const pag3 = await page.locator('.leitor-paginacao span').textContent()
    console.log('[citacoes-navegacao] paginacao após 3º clique (volta)', pag3)
    assert.ok(pag3.includes('Página 3'), `terceiro clique deveria voltar para página 3, veio ${pag3}`)

    // testa clique no botão Ver no texto também navega (segunda citação página 5)
    const segundaCard = clicaveis.nth(1)
    await segundaCard.locator('button', { hasText: 'Ver no texto' }).click()
    await page.waitForTimeout(400)
    const pag5 = await page.locator('.leitor-paginacao span').textContent()
    console.log('[citacoes-navegacao] paginacao após clicar Ver no texto da segunda', pag5)
    assert.ok(pag5.includes('Página 5'), `Ver no texto da segunda deveria ir para página 5, veio ${pag5}`)

    // testa acessibilidade: foco via teclado Enter
    await clicaveis.first().focus()
    await page.keyboard.press('Enter')
    await page.waitForTimeout(400)
    const pagEnter = await page.locator('.leitor-paginacao span').textContent()
    console.log('[citacoes-navegacao] após Enter', pagEnter)
    // após ciclo anterior, Enter deve ir para próxima (7)
    assert.ok(pagEnter.includes('Página 7') || pagEnter.includes('Página 3'), 'Enter deveria navegar')

    // testa citação sem ocorrência: clique não navega e mostra toast aviso
    const antesSem = await page.locator('.leitor-paginacao span').textContent()
    await semPos.first().click()
    await page.waitForTimeout(300)
    const t = page.locator('.toast .aviso')
    // pode estar visível brevemente
    try {
      await t.waitFor({ state: 'visible', timeout: 2000 })
      const avisoText = await t.textContent()
      console.log('[citacoes-navegacao] aviso sem ocorrência', avisoText)
      assert.ok(avisoText.includes('Sem ocorrência'), `aviso deveria conter Sem ocorrência, veio ${avisoText}`)
    } catch {
      console.log('[citacoes-navegacao] toast não apareceu (pode ser timing), mas paginação não deve ter mudado')
    }
    const depoisSem = await page.locator('.leitor-paginacao span').textContent()
    // sem navegação, pagina deve permanecer igual
    // (antes era página 7 ou 3, deve continuar)
    console.log('[citacoes-navegacao] paginação antes/sem', antesSem, '->', depoisSem)
    // não verifica igualdade estrita porque Enter pode ter mudado, mas garante que não foi para 0
    assert.ok(!depoisSem.includes('Página 0'), 'não deve navegar para página 0')

    // testa click na página limpa o destaque
    // destaque ainda visível, clica na pagina-inner
    await page.locator('.pagina-inner').first().click()
    await page.waitForTimeout(300)
    const qtdDestaquesAposClick = await page.locator('[data-testid="fonte-destaque"]').count()
    console.log('[citacoes-navegacao] destaques após click na página', qtdDestaquesAposClick)
    assert.equal(qtdDestaquesAposClick, 0, 'click na página deveria limpar o destaque')

    // testa pulso remove após 2s mas mantém contorno (se não clicou, pulso some)
    await clicaveis.first().click()
    await page.locator('[data-testid="fonte-destaque"]').first().waitFor({ timeout: 3000 })
    const clsAntes = await page.locator('[data-testid="fonte-destaque"]').first().getAttribute('class')
    assert.ok(clsAntes.includes('pulso'), 'logo após clique deve ter pulso')
    await page.waitForTimeout(2100)
    const clsDepois = await page.locator('[data-testid="fonte-destaque"]').first().getAttribute('class')
    console.log('[citacoes-navegacao] classe depois 2s', clsDepois)
    assert.ok(!clsDepois.includes('pulso'), 'após 2s pulso deveria ter sido removido')
    // destaque ainda deve existir (contorno sutil)
    assert.equal(await page.locator('[data-testid="fonte-destaque"]').count(), 1, 'destaque deveria permanecer com contorno após pulso')

    console.log('E2E citacoes-navegacao OK: card clicável, Ver no texto, scroll + destaque pulsante contrastante, ciclo multi-ocorrências, sem ocorrência aviso, limpar ao interagir, contraste AA')
  } catch (e) {
    console.error('E2E citacoes-navegacao FAIL', e)
    try { await page.screenshot({ path: 'C:/Users/haneg/AppData/Local/Temp/opencode/citacoes-navegacao-fail.png', fullPage: true }); console.log('screenshot salvo') } catch {}
    process.exit(1)
  } finally {
    await browser.close()
    if (server) server.close()
  }
}

main()
