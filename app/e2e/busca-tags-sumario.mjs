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
      console.error('[busca-tags-sumario] dist não encontrado e APP_URL não alcançável. Rode npm run build antes.')
      process.exit(1)
    }
    console.log('[busca-tags-sumario] iniciando servidor estático dist em 5173...')
    server = await startStaticServer(5173)
    for (let i = 0; i < 20; i++) {
      if (await isReachable(BASE)) break
      await new Promise(r => setTimeout(r, 200))
    }
  }

  // dados mockados
  const artigosMock = [
    { id: 1, titulo: 'Silva 2020 - Atenção e Memória', num_paginas: 5, criado_em: new Date().toISOString() },
    { id: 2, titulo: 'Outro Artigo Sem Relação', num_paginas: 3, criado_em: new Date().toISOString() },
    { id: 3, titulo: 'Estudo sobre Silva e métodos', num_paginas: 4, criado_em: new Date().toISOString() },
  ]
  let notasMock = [
    { id: 10, pagina: 1, texto: 'nota importante', criado_em: new Date().toISOString(), tags: ['revisar'], cor: '#FFEB3B' },
    { id: 11, pagina: 2, texto: 'outra nota', criado_em: new Date().toISOString(), tags: ['duvida'], cor: '#9EE6A8' },
  ]
  const sumarioMock = [
    { titulo: '1. Introdução', pagina: 1, nivel: 1, ordem: 1 },
    { titulo: '2. Metodologia', pagina: 3, nivel: 1, ordem: 2 },
    { titulo: 'Conclusão', pagina: 5, nivel: 1, ordem: 3 },
  ]

  const browser = await chromium.launch({ headless: true })
  const ctx = await browser.newContext()
  const page = await ctx.newPage()

  await page.route('**/api/**', async (route) => {
    const url = new URL(route.request().url())
    const pathname = url.pathname
    const method = route.request().method()

    if (pathname === '/api/health' || pathname === '/health') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true, versao: '0.1.0-test' }) })
    }
    if (pathname === '/api/auth/me' && method === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ id: 1, nome: 'Ana Bagatinii' }) })
    }
    if (pathname === '/api/auth/login' && method === 'POST') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ ok: true }), headers: { 'Set-Cookie': 'ana_session=mock.jwt; Path=/; HttpOnly' } })
    }
    if (pathname === '/api/auth/logout' && method === 'POST') {
      return route.fulfill({ status: 204, body: '' })
    }

    // listarArtigos com ?busca=
    if (pathname === '/api/artigos' && method === 'GET') {
      const busca = (url.searchParams.get('busca') || '').toLowerCase()
      let lista = artigosMock
      if (busca) {
        lista = artigosMock.filter(a => a.titulo.toLowerCase().includes(busca))
        // também simula notas tag matching: se busca === revisiar inclui artigo 1
        if (busca === 'revisar') lista = [artigosMock[0]]
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(lista) })
    }

    // getArtigo
    const mArtigo = pathname.match(/^\/api\/artigos\/(\d+)$/)
    if (mArtigo && method === 'GET') {
      const id = Number(mArtigo[1])
      const art = artigosMock.find(a => a.id === id) || artigosMock[0]
      return route.fulfill({
        status: 200, contentType: 'application/json',
        body: JSON.stringify({
          id: art.id,
          titulo: art.titulo,
          criado_em: art.criado_em,
          paginas: Array.from({ length: art.num_paginas }, (_, i) => ({ numero: i + 1, largura: 612, altura: 792 })),
        }),
      })
    }

    // camada
    const mCamada = pathname.match(/^\/api\/artigos\/\d+\/paginas\/(\d+)\/camada$/)
    if (mCamada && method === 'GET') {
      const palavras = [
        { texto: 'Olá', x0: 72, y0: 100, x1: 120, y1: 115 },
        { texto: 'mundo', x0: 125, y0: 100, x1: 180, y1: 115 },
        { texto: 'atenção', x0: 72, y0: 130, x1: 150, y1: 145 },
        { texto: 'teste', x0: 160, y0: 130, x1: 220, y1: 145 },
      ]
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ largura: 612, altura: 792, palavras }) })
    }

    // busca no PDF
    const mBusca = pathname.match(/^\/api\/artigos\/(\d+)\/busca$/)
    if (mBusca && method === 'GET') {
      const q = (url.searchParams.get('q') || '').toLowerCase()
      if (!q || q.length < 2) {
        return route.fulfill({ status: 400, contentType: 'application/json', body: JSON.stringify({ erro: 'q deve ter entre 2 e 100 caracteres' }) })
      }
      if (q === 'zzznenhum') {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
      }
      // retorna 3 ocorrências para "atenção" ou "mundo"
      if (q.includes('aten') || q.includes('mundo')) {
        return route.fulfill({
          status: 200, contentType: 'application/json',
          body: JSON.stringify([
            { pagina: 1, pos: [72, 130, 150, 145], trecho: '... atenção teste ...' },
            { pagina: 2, pos: [15, 30, 50, 45], trecho: '... atenção novamente ...' },
            { pagina: 3, pos: [10, 20, 40, 30], trecho: '... atenção terceira ...' },
          ]),
        })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    }

    // sumario
    const mSumario = pathname.match(/^\/api\/artigos\/(\d+)\/sumario$/)
    if (mSumario && method === 'GET') {
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(sumarioMock) })
    }

    // notas GET (com filtros ?tag=&cor=) e POST
    const mNotas = pathname.match(/^\/api\/artigos\/(\d+)\/notas$/)
    if (mNotas) {
      if (method === 'GET') {
        let lista = notasMock
        const tag = url.searchParams.get('tag')
        const cor = url.searchParams.get('cor')
        if (tag) lista = lista.filter(n => (n.tags || []).includes(tag))
        if (cor) lista = lista.filter(n => n.cor === cor)
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(lista) })
      }
      if (method === 'POST') {
        const body = JSON.parse(route.request().postData() || '{}')
        const nova = {
          id: Math.max(...notasMock.map(n => n.id), 10) + 1,
          pagina: body.pagina || 1,
          texto: body.texto || '',
          criado_em: new Date().toISOString(),
          tags: body.tags || [],
          cor: body.cor || '#FFEB3B',
          marcacao_id: body.marcacao_id,
        }
        notasMock.push(nova)
        return route.fulfill({ status: 201, contentType: 'application/json', body: JSON.stringify(nova) })
      }
    }

    // deletar nota
    const mDelNota = pathname.match(/^\/api\/artigos\/\d+\/notas\/(\d+)$/)
    if (mDelNota && method === 'DELETE') {
      const id = Number(mDelNota[1])
      notasMock = notasMock.filter(n => n.id !== id)
      return route.fulfill({ status: 204, body: '' })
    }

    // patch nota (atualizar tags/cor)
    if (mDelNota && method === 'PATCH') {
      const id = Number(mDelNota[1])
      const body = JSON.parse(route.request().postData() || '{}')
      const idx = notasMock.findIndex(n => n.id === id)
      if (idx >= 0) {
        if (body.tags) notasMock[idx].tags = body.tags
        if (body.cor) notasMock[idx].cor = body.cor
        if (body.texto) notasMock[idx].texto = body.texto
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify(notasMock[idx]) })
      }
      return route.fulfill({ status: 404, contentType: 'application/json', body: JSON.stringify({ erro: 'nota não encontrada' }) })
    }

    // citacoes, marcacoes, historico mock vazio
    if (pathname.includes('/citacoes') || pathname.includes('/marcacoes') || pathname.includes('/historico')) {
      if (method === 'POST' && pathname.includes('/varrer-citacoes')) {
        return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
      }
      return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify([]) })
    }

    // imagem
    const mImg = pathname.match(/^\/api\/artigos\/\d+\/paginas\/\d+\/imagem$/)
    if (mImg) {
      const png = Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+ip1sAAAAASUVORK5CYII=', 'base64')
      return route.fulfill({ status: 200, contentType: 'image/png', body: png })
    }

    return route.continue()
  })

  page.on('console', msg => console.log('[browser]', msg.text()))

  try {
    console.log('[busca-tags-sumario] navegando para', BASE)
    await page.goto(BASE, { waitUntil: 'domcontentloaded' })
    await page.waitForSelector('.biblioteca', { timeout: 10000 })

    // ---- 1. Biblioteca: digita busca → filtra e destaca ----
    console.log('[test] Biblioteca busca filtra e destaca')
    const buscaInput = page.locator('.biblioteca-busca input')
    await buscaInput.waitFor({ timeout: 5000 })
    // verifica contador inicial: 3 artigos
    let contador = page.locator('.biblioteca-contador')
    await contador.waitFor({ timeout: 3000 })
    let textoContador = await contador.textContent()
    console.log('[busca] contador inicial', textoContador)
    assert.ok(textoContador.includes('3 artigos') || textoContador.includes('3'), 'contador inicial deveria ser 3')

    // digita "Silva"
    await buscaInput.fill('Silva')
    await page.waitForTimeout(500) // debounce 300ms + fetch
    // aguarda lista filtrada (2 resultados: Silva 2020... e Estudo sobre Silva)
    await page.waitForTimeout(400)
    const cards = page.locator('.artigo-card')
    let count = await cards.count()
    console.log('[busca] cards após Silva', count)
    assert.equal(count, 2, `busca Silva deveria filtrar 2, veio ${count}`)

    // verifica destaque <mark>
    const marks = page.locator('.busca-mark')
    const markCount = await marks.count()
    console.log('[busca] marks', markCount)
    assert.ok(markCount >= 2, `esperado pelo menos 2 marks para Silva, veio ${markCount}`)
    const markTexto = await marks.first().textContent()
    assert.ok(markTexto.toLowerCase() === 'silva', `mark deveria ser Silva, veio ${markTexto}`)

    // verifica contador "2 resultados para \"Silva\""
    textoContador = await contador.textContent()
    console.log('[busca] contador após Silva', textoContador)
    assert.ok(textoContador.includes('2 resultados'), `contador deveria ser 2 resultados, veio ${textoContador}`)
    assert.ok(textoContador.includes('Silva'), `contador deveria conter Silva, veio ${textoContador}`)

    // race: digitação rápida (simula digitar rápido)
    await buscaInput.fill('Sil')
    await buscaInput.fill('Silva 2020')
    await page.waitForTimeout(500)
    count = await cards.count()
    console.log('[busca race] cards após digitação rápida Silva 2020', count)
    assert.equal(count, 1, `digitação rápida Silva 2020 deveria dar 1, veio ${count}`)

    // limpa busca
    await buscaInput.fill('')
    await page.waitForTimeout(500)
    count = await cards.count()
    assert.equal(count, 3, 'limpar busca deveria voltar a 3')

    // XSS busca não quebra
    await buscaInput.fill('<script>alert(1)</script>')
    await page.waitForTimeout(500)
    count = await cards.count()
    console.log('[busca XSS] cards', count)
    assert.equal(count, 0, 'XSS busca deveria retornar 0 sem quebrar')
    await buscaInput.fill('')
    await page.waitForTimeout(500)

    // abre artigo
    await page.locator('.artigo-card', { hasText: 'Silva 2020 - Atenção' }).first().click()
    await page.waitForSelector('.leitor', { timeout: 8000 })
    await page.waitForSelector('.painel', { timeout: 5000 })

    // ---- 2. Busca no PDF via Ctrl+K ----
    console.log('[test] Busca no PDF Ctrl+K')
    // abre barra via botão
    const btnBusca = page.locator('.leitor-toolbar button', { hasText: '' }).filter({ has: page.locator('svg') }).first()
    // mais seguro: clica no botão de busca pelo title
    await page.locator('button[title="Buscar no texto (Ctrl+K)"]').click()
    const barra = page.locator('.leitor-busca-barra')
    await barra.waitFor({ state: 'visible', timeout: 3000 })
    const inputBuscaPdf = barra.locator('input')
    await inputBuscaPdf.fill('atenção')
    await page.waitForTimeout(600) // debounce + fetch

    // verifica contador "3 ocorrências — 1/3"
    const contadorPdf = barra.locator('.leitor-busca-contador')
    let contadorPdfTexto = await contadorPdf.textContent()
    console.log('[busca pdf] contador', contadorPdfTexto)
    assert.ok(contadorPdfTexto.includes('3 ocorrências'), `deveria ser 3 ocorrências, veio ${contadorPdfTexto}`)
    assert.ok(contadorPdfTexto.includes('1/3'), `deveria ser 1/3, veio ${contadorPdfTexto}`)

    // verifica destaque pulsante no PDF (busca-destaque)
    let destaque = page.locator('[data-testid="busca-destaque"]')
    await destaque.first().waitFor({ state: 'visible', timeout: 5000 })
    let box = await destaque.first().boundingBox()
    assert.ok(box && box.width > 0, 'busca destaque deveria ter tamanho')
    let cls = await destaque.first().getAttribute('class')
    assert.ok(cls.includes('pulso'), 'busca destaque deveria ter pulso')
    console.log('[busca pdf] destaque visível', box)

    // verifica página atual foi para 1 (primeira ocorrência)
    let paginacao = await page.locator('.leitor-paginacao span').textContent()
    console.log('[busca pdf] paginação', paginacao)
    assert.ok(paginacao.includes('Página 1'), `deveria estar na página 1, veio ${paginacao}`)

    // próximo → 2/3
    await barra.locator('button[title="Próxima"]').click()
    await page.waitForTimeout(400)
    contadorPdfTexto = await contadorPdf.textContent()
    console.log('[busca pdf] após próximo', contadorPdfTexto)
    assert.ok(contadorPdfTexto.includes('2/3'), `deveria ser 2/3, veio ${contadorPdfTexto}`)
    paginacao = await page.locator('.leitor-paginacao span').textContent()
    assert.ok(paginacao.includes('Página 2'), `deveria ir para página 2, veio ${paginacao}`)

    // próximo → 3/3
    await barra.locator('button[title="Próxima"]').click()
    await page.waitForTimeout(400)
    contadorPdfTexto = await contadorPdf.textContent()
    assert.ok(contadorPdfTexto.includes('3/3'), `deveria ser 3/3, veio ${contadorPdfTexto}`)

    // anterior → 2/3
    await barra.locator('button[title="Anterior"]').click()
    await page.waitForTimeout(400)
    contadorPdfTexto = await contadorPdf.textContent()
    assert.ok(contadorPdfTexto.includes('2/3'), `anterior deveria voltar a 2/3, veio ${contadorPdfTexto}`)

    // Enter também navega (Shift+Enter anterior)
    await inputBuscaPdf.press('Enter')
    await page.waitForTimeout(400)
    contadorPdfTexto = await contadorPdf.textContent()
    assert.ok(contadorPdfTexto.includes('3/3'), 'Enter deveria ir para próximo (3/3)')

    // Esc fecha
    await page.keyboard.press('Escape')
    await page.waitForTimeout(200)
    assert.equal(await barra.count(), 0, 'Esc deveria fechar barra de busca')

    // testa Ctrl+K reabre
    await page.keyboard.press('Control+K')
    await barra.waitFor({ state: 'visible', timeout: 2000 })
    console.log('[busca pdf] Ctrl+K reabriu')
    await page.keyboard.press('Escape')
    await page.waitForTimeout(200)

    // ---- 3. Notas com tags/cor filtro ----
    console.log('[test] Notas tags/cor')
    // abre aba Notas
    await page.locator('.painel-tab', { hasText: 'Notas' }).click()
    await page.waitForTimeout(300)

    // verifica notas existentes 2
    let notaCards = page.locator('.nota-card')
    let notaCount = await notaCards.count()
    console.log('[notas] iniciais', notaCount)
    assert.equal(notaCount, 2, `esperado 2 notas iniciais, veio ${notaCount}`)

    // verifica filtro por tag chips
    const filtroRevisar = page.locator('.chip-tag', { hasText: 'revisar' })
    if (await filtroRevisar.count() > 0) {
      await filtroRevisar.first().click()
      await page.waitForTimeout(300)
      notaCount = await notaCards.count()
      console.log('[notas] após filtrar revisiar', notaCount)
      assert.equal(notaCount, 1, 'filtro revisiar deveria dar 1 nota')
      // verifica contador
      const filtroContador = page.locator('.filtro-contador')
      const filtroTexto = await filtroContador.textContent()
      assert.ok(filtroTexto.includes('1 de 2'), `contador filtro deveria ser 1 de 2, veio ${filtroTexto}`)
      // limpar
      await page.locator('.filtro-contador button', { hasText: 'Limpar' }).click()
      await page.waitForTimeout(300)
      assert.equal(await notaCards.count(), 2, 'limpar deveria voltar a 2')
    }

    // verifica filtro por cor
    const corChips = page.locator('.chip-cor')
    if (await corChips.count() > 0) {
      await corChips.first().click()
      await page.waitForTimeout(300)
      notaCount = await notaCards.count()
      console.log('[notas] após filtro cor', notaCount)
      assert.ok(notaCount >= 1, 'filtro cor deveria ter pelo menos 1')
      await page.locator('.filtro-contador button', { hasText: 'Limpar' }).click()
    }

    // criar nova nota com tags e cor via composer
    const textarea = page.locator('.painel-composer textarea')
    await textarea.fill('nota teste com tags')
    // adiciona tag "importante"
    const tagInput = page.locator('.input-tag')
    await tagInput.fill('importante')
    await page.locator('.composer-tags-linha button', { hasText: 'Adicionar' }).click()
    // verifica chip
    const chipImportante = page.locator('.composer-tags-chips .chip-tag', { hasText: 'importante' })
    await chipImportante.waitFor({ timeout: 2000 })
    // seleciona cor verde
    await page.locator('.composer-cor').nth(2).click()
    // salva
    await page.locator('.painel-composer .btn-primary', { hasText: 'Salvar nota' }).click()
    await page.waitForTimeout(600)
    notaCount = await notaCards.count()
    console.log('[notas] após criar com tag', notaCount)
    assert.equal(notaCount, 3, `deveria ter 3 notas após criar, veio ${notaCount}`)

    // verifica badge de tag na nova nota
    const ultimaNota = notaCards.last()
    const tagBadge = ultimaNota.locator('.nota-tag', { hasText: 'importante' })
    await tagBadge.waitFor({ timeout: 2000 })
    console.log('[notas] badge tag ok')

    // filtra por nova tag
    await page.locator('.chip-tag', { hasText: 'importante' }).first().click()
    await page.waitForTimeout(300)
    assert.equal(await notaCards.count(), 1, 'filtro importante deveria dar 1 (nova nota)')
    await page.locator('.filtro-contador button', { hasText: 'Limpar' }).click()

    // ---- 4. Sumário ----
    console.log('[test] Sumário')
    await page.locator('.painel-tab', { hasText: 'Sumário' }).click()
    await page.waitForTimeout(400)
    const sumarioItens = page.locator('.sumario-item')
    const sumarioCount = await sumarioItens.count()
    console.log('[sumario] count', sumarioCount)
    assert.equal(sumarioCount, 3, `sumário deveria ter 3 itens, veio ${sumarioCount}`)

    // verifica ordem
    const titulos = await sumarioItens.locator('.sumario-titulo').allTextContents()
    console.log('[sumario] títulos', titulos)
    assert.deepEqual(titulos, ['1. Introdução', '2. Metodologia', 'Conclusão'])
    const paginas = await sumarioItens.locator('.sumario-pagina').allTextContents()
    assert.deepEqual(paginas, ['p. 1', 'p. 3', 'p. 5'])

    // clica no segundo item (Metodologia p.3) → deve ir para página 3
    await sumarioItens.nth(1).click()
    await page.waitForTimeout(600)
    paginacao = await page.locator('.leitor-paginacao span').textContent()
    console.log('[sumario] paginação após clique Metodologia', paginacao)
    assert.ok(paginacao.includes('Página 3'), `clicar Metodologia deveria ir para página 3, veio ${paginacao}`)

    // clica no terceiro (Conclusão p.5)
    await sumarioItens.nth(2).click()
    await page.waitForTimeout(600)
    paginacao = await page.locator('.leitor-paginacao span').textContent()
    assert.ok(paginacao.includes('Página 5'), `clicar Conclusão deveria ir para página 5, veio ${paginacao}`)

    console.log('E2E busca-tags-sumario OK: biblioteca busca+mark+contador, PDF busca Ctrl+K + destaque pulsante + 1/3, notas tags/cor filtro, sumário ordem + scroll')
  } catch (e) {
    console.error('E2E busca-tags-sumario FAIL', e)
    try { await page.screenshot({ path: 'C:/Users/haneg/AppData/Local/Temp/opencode/busca-tags-sumario-fail.png', fullPage: true }); console.log('screenshot salvo') } catch {}
    process.exit(1)
  } finally {
    await browser.close()
    if (server) server.close()
  }
}

main()
