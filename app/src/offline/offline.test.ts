import { describe, it, expect, beforeEach, vi } from 'vitest'
import { __clearMemory } from './db'
import { saveOfflineSession, loadOfflineSession, loadLatestValidSession, clearOfflineSession, getOrCreateDeviceId, getCursor, setCursor } from './session'
import { cacheArtigos, loadArtigos, cacheArtigoDetalhe, loadArtigoDetalhe, cacheCamada, loadCamada, cacheImagem, loadImagem, cacheNotas, loadNotas, cacheMarcacoes, loadMarcacoes, upsertCachedNota, cacheBusca, loadBusca, cacheSumario, loadSumario } from './cache'
import { enqueueOperation, getPendingCount, syncNow, __resetSyncState, subscribeSyncStatus } from './sync'
import { outboxGetAll } from './db'
import type { ArtigoResumo } from '../api'

function artigo(id: number, titulo = `Artigo ${id}`): ArtigoResumo {
  return { id, titulo, num_paginas: 10, criado_em: new Date().toISOString() }
}

describe('offline cache e sessão', () => {
  beforeEach(() => {
    __clearMemory()
    __resetSyncState()
    vi.restoreAllMocks()
  })

  it('sessão persiste 7 dias e isola por usuário', async () => {
    await saveOfflineSession({ id: 1, nome: 'Ana' })
    await saveOfflineSession({ id: 2, nome: 'Bruno' })
    const s1 = await loadOfflineSession(1)
    const s2 = await loadOfflineSession(2)
    expect(s1?.nome).toBe('Ana')
    expect(s2?.nome).toBe('Bruno')
    // limpa apenas um
    await clearOfflineSession(1)
    expect(await loadOfflineSession(1)).toBeNull()
    expect((await loadOfflineSession(2))?.nome).toBe('Bruno')
  })

  it('sessão expira após 7 dias', async () => {
    const expired = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000).toISOString()
    const future = new Date(Date.now() + 8 * 24 * 60 * 60 * 1000).toISOString()
    await saveOfflineSession({ id: 10, nome: 'Old', onlineAt: expired, expiresAt: expired })
    expect(await loadOfflineSession(10)).toBeNull()
    await saveOfflineSession({ id: 11, nome: 'Fresh', onlineAt: new Date().toISOString(), expiresAt: future })
    expect((await loadOfflineSession(11))?.nome).toBe('Fresh')
    const latest = await loadLatestValidSession()
    expect(latest?.id).toBe(11)
  })

  it('deviceId estável', async () => {
    const a = await getOrCreateDeviceId()
    const b = await getOrCreateDeviceId()
    expect(a).toBe(b)
    expect(typeof a).toBe('string')
  })

  it('cache artigos isola por userId', async () => {
    await cacheArtigos(1, [artigo(1, 'A'), artigo(2, 'B')])
    await cacheArtigos(2, [artigo(3, 'C')])
    const u1 = await loadArtigos(1)
    const u2 = await loadArtigos(2)
    expect(u1.map(x=>x.id).sort()).toEqual([1,2])
    expect(u2.map(x=>x.id)).toEqual([3])
  })

  it('detalhe, camada, imagem blob, busca, sumario cacheiam e carregam', async () => {
    const det = { id: 5, titulo: 'T', criado_em: new Date().toISOString(), paginas: [{ numero:1, largura:612, altura:792 }] }
    await cacheArtigoDetalhe(1, det as never)
    expect((await loadArtigoDetalhe(1,5))?.titulo).toBe('T')
    const camada = { largura:612, altura:792, palavras: [{ texto:'a', x0:0,y0:0,x1:10,y1:10 }] }
    await cacheCamada(1,5,1, camada as never)
    expect((await loadCamada(1,5,1))?.largura).toBe(612)
    const blob = new Blob(['hello'], { type: 'image/png' })
    await cacheImagem(1,5,1, blob)
    const got = await loadImagem(1,5,1)
    expect(got?.size).toBe(blob.size)
    await cacheBusca(1,5,'ana',[ { pagina:1, pos:[0,0,10,10], trecho:'ana' } ])
    expect((await loadBusca(1,5,'Ana'))?.length).toBe(1)
    await cacheSumario(1,5,[{ titulo:'Intro', pagina:1, nivel:1, ordem:0 }])
    expect((await loadSumario(1,5))?.[0].titulo).toBe('Intro')
  })

  it('notas e marcações isolam por usuário e artigo', async () => {
    await cacheNotas(1, 10, [{ id:1, pagina:1, texto:'n1', criado_em:new Date().toISOString(), tags:[], cor:'#fff' }])
    await cacheNotas(1, 11, [{ id:2, pagina:1, texto:'n2', criado_em:new Date().toISOString(), tags:[], cor:'#fff' }])
    await cacheNotas(2, 10, [{ id:3, pagina:1, texto:'n3', criado_em:new Date().toISOString(), tags:[], cor:'#fff' }])
    expect((await loadNotas(1,10)).length).toBe(1)
    expect((await loadNotas(1,11))[0].id).toBe(2)
    expect((await loadNotas(2,10))[0].id).toBe(3)
    await cacheMarcacoes(1,10,[{ id:100, pagina:1, tipo:'highlight', cor:'#FFE03B', palavras:[], texto:'h' }])
    expect((await loadMarcacoes(1,10))[0].id).toBe(100)
    expect((await loadMarcacoes(1,11)).length).toBe(0)
  })

  it('enqueue usa id temporário negativo e clientId, pending count reflete', async () => {
    const opId = await enqueueOperation({ userId:1, artigoId:10, entity:'marcacao', action:'create', data:{ pagina:1 }, baseVersion:0, id:-123, clientId:'c-1' })
    expect(typeof opId).toBe('string')
    expect(await getPendingCount(1)).toBe(1)
    expect(await getPendingCount(2)).toBe(0)
    const all = await outboxGetAll() as unknown as { clientId:string, id:number }[]
    expect(all[0].clientId).toBe('c-1')
    expect(all[0].id).toBe(-123)
  })

  it('cursor por usuário é isolado', async () => {
    await setCursor(1, 'cursor-1')
    await setCursor(2, 'cursor-2')
    expect(await getCursor(1)).toBe('cursor-1')
    expect(await getCursor(2)).toBe('cursor-2')
  })

  it('sync applied remapeia id temporário e atualiza nota.marcacao_id (via outbox)', async () => {
    // cria marcacao temporária
    await enqueueOperation({ userId:1, artigoId:5, entity:'marcacao', action:'create', data:{ pagina:1, cor:'#FFE03B', palavras:[], texto:'t' }, baseVersion:0, id:-999, clientId:'cli-marc-1' })
    await upsertCachedNota(1,5, { id:-888, pagina:1, texto:'nota ligada', criado_em:new Date().toISOString(), tags:[], cor:'#fff', marcacao_id:-999 } as never, { clientId:'cli-nota-1', version:0 })
    await enqueueOperation({ userId:1, artigoId:5, entity:'nota', action:'create', data:{ pagina:1, texto:'nota ligada', marcacao_id:-999 }, baseVersion:0, id:-888, clientId:'cli-nota-1' })

    const origFetch = globalThis.fetch
    // mock /api/sync push + pull
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/sync') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body))
        // responde applied com novo id 500 para marcacao e 600 para nota
        const results = body.operations.map((op: { opId:string, clientId:string, entity:string }) => {
          if (op.entity === 'marcacao') return { opId: op.opId, status:'applied', id:500, clientId: op.clientId, version:1, serverData:{ id:500, pagina:1, tipo:'highlight', cor:'#FFE03B', palavras:[], texto:'t' } }
          return { opId: op.opId, status:'applied', id:600, clientId: op.clientId, version:1, serverData:{ id:600, pagina:1, texto:'nota ligada', criado_em:new Date().toISOString(), tags:[], cor:'#fff', marcacao_id:500 } }
        })
        return new Response(JSON.stringify({ results }), { status:200, headers:{ 'Content-Type':'application/json' } })
      }
      if (u.includes('/api/sync/pull')) {
        return new Response(JSON.stringify({ changes:[], cursor:'new-cursor', hasMore:false }), { status:200, headers:{ 'Content-Type':'application/json' } })
      }
      return new Response('{}', { status:200 })
    }) as unknown as typeof fetch

    await syncNow(1)
    // com gate de dependência, primeira sync envia apenas marcação; nota dependente fica pendente com marcacao_id remapeado após handleApplied
    await syncNow(1)
    expect(await getPendingCount(1)).toBe(0)
    const notas = await loadNotas(1,5)
    // deve ter nota com marcacao_id remapeado para 500 e id 600
    const nota = notas.find(n=> n.id===600)
    expect(nota?.marcacao_id).toBe(500)
    const marcs = await loadMarcacoes(1,5)
    expect(marcs.some(m=> m.id===500)).toBe(true)
    expect(marcs.some(m=> m.id===-999)).toBe(false)

    globalThis.fetch = origFetch
  })

  it('sync conflict preserva dado local e marca status', async () => {
    await enqueueOperation({ userId:1, artigoId:7, entity:'nota', action:'update', data:{ texto:'novo' }, baseVersion:1, id:10, clientId:'cli-10' })
    await upsertCachedNota(1,7, { id:10, pagina:1, texto:'local', criado_em:new Date().toISOString(), tags:[], cor:'#fff' } as never, { clientId:'cli-10', version:1 })

    const origFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/sync') && init?.method === 'POST') {
        const body = JSON.parse(String(init.body))
        const results = body.operations.map((op: { opId:string })=> ({ opId: op.opId, status:'conflict', error:'versão desatualizada', serverData:{ id:10, texto:'server' } }))
        return new Response(JSON.stringify({ results }), { status:200, headers:{ 'Content-Type':'application/json' } })
      }
      if (u.includes('/api/sync/pull')) return new Response(JSON.stringify({ changes:[], cursor:'c', hasMore:false }), { status:200 })
      return new Response('{}', { status:200 })
    }) as unknown as typeof fetch

    await syncNow(1)
    const ops = await outboxGetAll() as unknown as { status:string, error:string }[]
    expect(ops.some(o=> o.status==='conflict')).toBe(true)
    const notas = await loadNotas(1,7)
    expect(notas.find(n=> n.id===10)?.texto).toBe('local')

    globalThis.fetch = origFetch
  })

  it('pull aplica tombstone e não sobrescreve pendente local', async () => {
    await upsertCachedNota(1,9, { id:20, pagina:1, texto:'keep', criado_em:new Date().toISOString(), tags:[], cor:'#fff' } as never, { clientId:'cli-keep', version:5 })
    await enqueueOperation({ userId:1, artigoId:9, entity:'nota', action:'update', data:{ texto:'pending edit' }, baseVersion:5, id:20, clientId:'cli-keep' })
    // também upsert nota 30 que será deletada via pull
    await upsertCachedNota(1,9, { id:30, pagina:1, texto:'to delete', criado_em:new Date().toISOString(), tags:[], cor:'#fff' } as never, { clientId:'cli-del', version:2 })

    const origFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/sync') && init?.method === 'POST') {
        // sem pendentes? já vamos direto para pull; mas push será feito e não tem resultado? vamos retornar vazio
        return new Response(JSON.stringify({ results: [] }), { status:200 })
      }
      if (u.includes('/api/sync/pull')) {
        const changes = [
          { entity:'nota', id:30, clientId:'cli-del', version:3, updatedAt:new Date().toISOString(), deleted:true, data:null },
          { entity:'nota', id:20, clientId:'cli-keep', version:6, updatedAt:new Date().toISOString(), deleted:false, data:{ id:20, pagina:1, texto:'server overwrite', criado_em:new Date().toISOString(), tags:[], cor:'#fff' } },
          { entity:'nota', id:31, clientId:'cli-new', version:1, updatedAt:new Date().toISOString(), deleted:false, data:{ id:31, pagina:1, texto:'new remote', criado_em:new Date().toISOString(), tags:[], cor:'#fff', artigo_id:9 } },
        ]
        return new Response(JSON.stringify({ changes, cursor:'c2', hasMore:false }), { status:200 })
      }
      return new Response('{}', { status:200 })
    }) as unknown as typeof fetch

    // primeiro faz sync que vai pull
    // precisa limpar o outbox do teste anterior? faz syncNow que fará push vazio e pull
    // mas temos pendente para 20, pull deve ignorar id 20
    await syncNow(1)
    const notas = await loadNotas(1,9)
    expect(notas.some(n=> n.id===30)).toBe(false) // tombstone removido
    expect(notas.find(n=> n.id===20)?.texto).toBe('keep') // não sobrescreveu pendente
    expect(notas.some(n=> n.id===31)).toBe(true) // novo remoto aplicado

    globalThis.fetch = origFetch
  })

  it('single-flight não roda dois sync simultâneos (batch limitado)', async () => {
    await enqueueOperation({ userId:1, artigoId:1, entity:'nota', action:'create', data:{}, baseVersion:0, id:-1, clientId:'c1' })
    await enqueueOperation({ userId:1, artigoId:1, entity:'nota', action:'create', data:{}, baseVersion:0, id:-2, clientId:'c2' })

    let callCount = 0
    const origFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (url: unknown, init?: RequestInit) => {
      const u = String(url)
      if (u.includes('/api/sync') && init?.method === 'POST') {
        callCount++
        await new Promise(r=> setTimeout(r, 30))
        const body = JSON.parse(String(init.body))
        const results = body.operations.map((op:{opId:string})=> ({ opId:op.opId, status:'applied', id: Math.floor(Math.random()*1000+100), clientId:'x', version:1 }))
        return new Response(JSON.stringify({ results }), { status:200 })
      }
      if (u.includes('/api/sync/pull')) return new Response(JSON.stringify({ changes:[], cursor:'c', hasMore:false }), { status:200 })
      return new Response('{}',{status:200})
    }) as unknown as typeof fetch

    await Promise.all([syncNow(1), syncNow(1)])
    expect(callCount).toBe(1)
    globalThis.fetch = origFetch
  })

  it('subscribeSyncStatus notifica pending/conflict', async () => {
    const states: number[] = []
    const unsub = subscribeSyncStatus(s=> states.push(s.pending))
    await enqueueOperation({ userId:5, artigoId:1, entity:'nota', action:'create', data:{}, baseVersion:0, id:-10 })
    await new Promise(r=> setTimeout(r, 10))
    expect(states[states.length-1]).toBe(1)
    unsub()
  })
})
