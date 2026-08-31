import { describe, it, expect, beforeEach, vi } from 'vitest'
import { __clearMemory } from './db'
import { upsertCachedMarcacao, upsertCachedNota, getCachedMarcVersion, getCachedNotaVersion, deleteCachedMarcacao } from './cache'
import { enqueueOperation, syncNow, __resetSyncState, cancelPendingOperationsForTempId, mergePendingCreateData, getPendingCount } from './sync'
import { outboxGetAll } from './db'

describe('sync-fix: correções offline', () => {
  beforeEach(() => {
    __clearMemory()
    __resetSyncState()
    vi.restoreAllMocks()
  })

  it('cancelar create temporário remove outbox correspondente e não deixa fantasma', async () => {
    await enqueueOperation({ userId: 1, artigoId: 10, entity: 'marcacao', action: 'create', data: { artigo_id: 10, pagina: 1, cor: '#FF0000', palavras: [], texto: 't' }, baseVersion: 0, id: -123, clientId: 'cli-123' })
    await enqueueOperation({ userId: 1, artigoId: 10, entity: 'marcacao', action: 'update', data: { cor: '#00FF00' }, baseVersion: 0, id: -123, clientId: 'cli-123' })
    expect(await getPendingCount(1)).toBe(2)
    // simula removerMarcacao temporária: cancela todas pendentes para tempId
    await cancelPendingOperationsForTempId(1, 10, 'marcacao', -123)
    expect(await getPendingCount(1)).toBe(0)
    const all = await outboxGetAll() as unknown as { id:number }[]
    expect(all.length).toBe(0)

    // mesma coisa para nota temporária
    await enqueueOperation({ userId: 1, artigoId: 10, entity: 'nota', action: 'create', data: { artigo_id: 10, pagina: 1, texto: 'n' }, baseVersion: 0, id: -999, clientId: 'cli-nota' })
    expect(await getPendingCount(1)).toBe(1)
    await cancelPendingOperationsForTempId(1, 10, 'nota', -999)
    expect(await getPendingCount(1)).toBe(0)
  })

  it('atualizar create temporário faz merge da cor em vez de criar update separado', async () => {
    await enqueueOperation({ userId: 1, artigoId: 5, entity: 'marcacao', action: 'create', data: { artigo_id: 5, pagina: 1, cor: '#FFE03B', palavras: [], texto: 't' }, baseVersion: 0, id: -555, clientId: 'cli-marc' })
    await mergePendingCreateData(1, 5, 'marcacao', -555, { cor: '#FF0000' })
    const all = await outboxGetAll() as unknown as { data: Record<string, unknown> }[]
    expect(all.length).toBe(1)
    expect(all[0].data.cor).toBe('#FF0000')
    expect(all[0].data.pagina).toBe(1) // preserva outros campos
  })

  it('captura versão antes do delete: leitura antes de apagar garante baseVersion/clientId', async () => {
    // cria cache com versão 7
    await upsertCachedMarcacao(1, 10, { id: 100, pagina: 1, tipo: 'highlight', cor: '#FFE03B', palavras: [], texto: 't' }, { clientId: 'cli-100', version: 7 })
    await upsertCachedNota(1, 10, { id: 200, pagina: 1, texto: 'nota', criado_em: new Date().toISOString(), tags: [], cor: '#fff' }, { clientId: 'cli-200', version: 9 })

    const verMarcAntes = await getCachedMarcVersion(1, 10, 100)
    const verNotaAntes = await getCachedNotaVersion(1, 10, 200)
    expect(verMarcAntes?.version).toBe(7)
    expect(verMarcAntes?.clientId).toBe('cli-100')
    expect(verNotaAntes?.version).toBe(9)

    // simula o fluxo corrigido: lê antes, depois apaga
    const verParaDelete = await getCachedMarcVersion(1, 10, 100).catch(() => null)
    await deleteCachedMarcacao(1, 10, 100)
    const apos = await getCachedMarcVersion(1, 10, 100).catch(() => null)
    expect(apos).toBeNull() // após delete não há versão
    expect(verParaDelete?.version).toBe(7) // mas a capturada antes permanece
    // enqueue delete usaria verParaDelete
    await enqueueOperation({ userId: 1, artigoId: 10, entity: 'marcacao', action: 'delete', data: {}, baseVersion: verParaDelete?.version ?? 0, id: 100, clientId: verParaDelete?.clientId ?? 'x' })
    const ops = await outboxGetAll() as unknown as { baseVersion:number, clientId:string }[]
    expect(ops[0].baseVersion).toBe(7)
    expect(ops[0].clientId).toBe('cli-100')
  })

  it('não envia nota com marcacao_id negativo enquanto criação da marcação estiver pendente', async () => {
    await enqueueOperation({ userId: 1, artigoId: 5, entity: 'marcacao', action: 'create', data: { artigo_id: 5, pagina: 1, cor: '#FFE03B', palavras: [], texto: 't' }, baseVersion: 0, id: -999, clientId: 'cli-marc-1' })
    await enqueueOperation({ userId: 1, artigoId: 5, entity: 'nota', action: 'create', data: { artigo_id: 5, pagina: 1, texto: 'nota ligada', marcacao_id: -999 }, baseVersion: 0, id: -888, clientId: 'cli-nota-1' })
    // terceira nota independente não deve ser bloqueada? vamos colocar uma sem dependência pendente
    await enqueueOperation({ userId: 1, artigoId: 5, entity: 'nota', action: 'create', data: { artigo_id: 5, pagina: 1, texto: 'nota livre' }, baseVersion: 0, id: -777, clientId: 'cli-nota-2' })

    let pushedOps: unknown[] = []
    const origFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/sync') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body))
        pushedOps = body.operations
        // responde applied para marcacao apenas (primeiro lote)
        const results = body.operations.map((op: { opId:string, entity:string, clientId:string }) => {
          if (op.entity === 'marcacao') return { opId: op.opId, status:'applied', id:500, clientId: op.clientId, version:1, serverData:{ id:500, pagina:1, tipo:'highlight', cor:'#FFE03B', palavras:[], texto:'t' } }
          return { opId: op.opId, status:'applied', id:600 + Math.floor(Math.random()*10), clientId: op.clientId, version:1, serverData:{ id:600, pagina:1, texto:'x', criado_em:new Date().toISOString(), tags:[], cor:'#fff' } }
        })
        return new Response(JSON.stringify({ results }), { status:200, headers:{ 'Content-Type':'application/json' } })
      }
      if (u.includes('/api/sync/pull')) return new Response(JSON.stringify({ changes:[], cursor:'c', hasMore:false }), { status:200 })
      return new Response('{}',{status:200})
    }) as unknown as typeof fetch

    await syncNow(1)

    // primeiro sync deve ter enviado apenas marcacao -999 e nota livre -777, mas NÃO a nota dependente -888
    // verifica que nenhuma operação enviada continha marcacao_id negativo -999
    const hasBlockedMarcacao = (pushedOps as Array<{ data: Record<string, unknown>} >).some(o => o.data?.marcacao_id === -999)
    expect(hasBlockedMarcacao).toBe(false)
    // deve ter enviado 2 ops: marcacao cria e nota livre (não a dependente)
    expect((pushedOps as Array<unknown>).length).toBe(2)
    // pending deve ainda conter a nota dependente, mas com marcacao_id remapeado para 500 após handleApplied
    expect(await getPendingCount(1)).toBe(1)
    const remaining = await outboxGetAll() as unknown as { data: Record<string, unknown>} []
    expect(remaining[0].data.marcacao_id).toBe(500)

    globalThis.fetch = origFetch
  })

  it('create de nota/marcacao carrega artigo_id compatível com backend', async () => {
    // verifica que enqueue com artigo_id funciona no push
    await enqueueOperation({ userId: 1, artigoId: 9, entity: 'marcacao', action: 'create', data: { artigo_id: 9, artigoId: 9, pagina: 1, cor: '#FFE03B', palavras: [], texto: 't' }, baseVersion: 0, id: -1 })
    await enqueueOperation({ userId: 1, artigoId: 9, entity: 'nota', action: 'create', data: { artigo_id: 9, artigoId: 9, pagina: 1, texto: 'n', marcacao_id: 1 }, baseVersion: 0, id: -2 })
    const all = await outboxGetAll() as unknown as { data: Record<string, unknown>, artigoId:number }[]
    expect(all.every(o => o.data.artigo_id === 9 || o.data.artigoId === 9)).toBe(true)
    // simula server exigindo artigo_id presente
    const allData = all.map(o => o.data)
    expect(allData[0].artigo_id).toBe(9)
    expect(allData[1].artigo_id).toBe(9)
  })
})
