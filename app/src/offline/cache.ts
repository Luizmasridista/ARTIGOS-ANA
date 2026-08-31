import type { ArtigoDetalhe, ArtigoResumo, BuscaResultado, CamadaTexto, Citacao, HistoricoEvento, Marcacao, Nota, SumarioItem } from '../api'
import { storeDeleteRaw, storeGetAllByPrefixRaw, storeGetRaw, storePutRaw } from './db'

// helpers key builders: `${userId}:${...}` isolando por usuario
function artigoKey(userId: number, artigoId: number): string { return `${userId}:${artigoId}` }
function artigosPrefix(userId: number): string { return `${userId}:` }
function camadaKey(userId: number, artigoId: number, pagina: number): string { return `${userId}:${artigoId}:${pagina}` }
function imagemKey(userId: number, artigoId: number, pagina: number): string { return `${userId}:${artigoId}:${pagina}` }
function notaKey(userId: number, artigoId: number, notaId: number): string { return `${userId}:${artigoId}:${notaId}` }
function marcacaoKey(userId: number, artigoId: number, marcId: number): string { return `${userId}:${artigoId}:${marcId}` }

// ArtigoResumo lista
export async function cacheArtigos(userId: number, artigos: ArtigoResumo[]): Promise<void> {
  for (const a of artigos) {
    await storePutRaw('artigos', { key: artigoKey(userId, a.id), userId, artigoId: a.id, data: a } )
  }
  // also persist ids list for quick has? not needed
}
export async function loadArtigos(userId: number): Promise<ArtigoResumo[]> {
  const raws = await storeGetAllByPrefixRaw('artigos', artigosPrefix(userId))
  return raws.map(r => (r as { data: ArtigoResumo }).data).sort((a,b)=> b.id - a.id)
}
export async function cacheArtigoDetalhe(userId: number, detalhe: ArtigoDetalhe): Promise<void> {
  await storePutRaw('detalhes', { key: artigoKey(userId, detalhe.id), userId, artigoId: detalhe.id, data: detalhe })
}
export async function loadArtigoDetalhe(userId: number, artigoId: number): Promise<ArtigoDetalhe | null> {
  const raw = await storeGetRaw('detalhes', artigoKey(userId, artigoId)) as { data: ArtigoDetalhe } | undefined
  return raw?.data ?? null
}

// camada
export async function cacheCamada(userId: number, artigoId: number, pagina: number, camada: CamadaTexto): Promise<void> {
  await storePutRaw('camadas', { key: camadaKey(userId, artigoId, pagina), userId, artigoId, pagina, data: camada })
}
export async function loadCamada(userId: number, artigoId: number, pagina: number): Promise<CamadaTexto | null> {
  const raw = await storeGetRaw('camadas', camadaKey(userId, artigoId, pagina)) as { data: CamadaTexto } | undefined
  return raw?.data ?? null
}

// imagens Blob
export async function cacheImagem(userId: number, artigoId: number, pagina: number, blob: Blob): Promise<void> {
  await storePutRaw('imagens', { key: imagemKey(userId, artigoId, pagina), userId, artigoId, pagina, blob })
}
export async function loadImagem(userId: number, artigoId: number, pagina: number): Promise<Blob | null> {
  const raw = await storeGetRaw('imagens', imagemKey(userId, artigoId, pagina)) as { blob: Blob } | undefined
  return raw?.blob ?? null
}

// notas - armazenadas individualmente com wrap contendo version/clientId
export interface CachedNota {
  key: string
  userId: number
  artigoId: number
  id: number
  clientId: string | null
  version: number
  updatedAt: string
  data: Nota
  deleted?: boolean
}
export async function cacheNotas(userId: number, artigoId: number, notas: Nota[]): Promise<void> {
  // keep version/clientId if already cached, else defaults
  for (const n of notas) {
    const existing = await storeGetRaw('notas', notaKey(userId, artigoId, n.id)) as unknown as CachedNota | undefined
    const val: CachedNota = {
      key: notaKey(userId, artigoId, n.id),
      userId, artigoId, id: n.id,
      clientId: existing?.clientId ?? null,
      version: existing?.version ?? 0,
      updatedAt: n.criado_em ?? new Date().toISOString(),
      data: n,
    }
    await storePutRaw('notas', val as unknown as Record<string, unknown>)
  }
}
export async function loadNotas(userId: number, artigoId: number): Promise<Nota[]> {
  const raws = await storeGetAllByPrefixRaw('notas', `${userId}:${artigoId}:`)
  const filtered = raws as unknown as CachedNota[]
  return filtered.filter(r => !r.deleted).map(r => r.data).sort((a,b)=> a.id - b.id)
}
export async function upsertCachedNota(userId: number, artigoId: number, nota: Nota, opts?: { clientId?: string | null, version?: number }): Promise<void> {
  const k = notaKey(userId, artigoId, nota.id)
  const prev = await storeGetRaw('notas', k) as unknown as CachedNota | undefined
  const val: CachedNota = {
    key: k, userId, artigoId, id: nota.id,
    clientId: opts?.clientId ?? prev?.clientId ?? null,
    version: opts?.version ?? prev?.version ?? 0,
    updatedAt: nota.criado_em ?? new Date().toISOString(),
    data: nota,
  }
  await storePutRaw('notas', val as unknown as Record<string, unknown>)
}
export async function deleteCachedNota(userId: number, artigoId: number, notaId: number): Promise<void> {
  await storeDeleteRaw('notas', notaKey(userId, artigoId, notaId))
}

// marcacoes
export interface CachedMarc {
  key: string
  userId: number
  artigoId: number
  id: number
  clientId: string | null
  version: number
  updatedAt: string
  data: Marcacao
  deleted?: boolean
}
export async function cacheMarcacoes(userId: number, artigoId: number, marcs: Marcacao[]): Promise<void> {
  for (const m of marcs) {
    const existing = await storeGetRaw('marcacoes', marcacaoKey(userId, artigoId, m.id)) as unknown as CachedMarc | undefined
    const val: CachedMarc = {
      key: marcacaoKey(userId, artigoId, m.id),
      userId, artigoId, id: m.id,
      clientId: existing?.clientId ?? null,
      version: existing?.version ?? 0,
      updatedAt: new Date().toISOString(),
      data: m,
    }
    await storePutRaw('marcacoes', val as unknown as Record<string, unknown>)
  }
}
export async function loadMarcacoes(userId: number, artigoId: number): Promise<Marcacao[]> {
  const raws = await storeGetAllByPrefixRaw('marcacoes', `${userId}:${artigoId}:`)
  const filtered = raws as unknown as CachedMarc[]
  return filtered.filter(r=>!r.deleted).map(r=>r.data).sort((a,b)=>a.id-b.id)
}
export async function upsertCachedMarcacao(userId: number, artigoId: number, m: Marcacao, opts?: { clientId?: string | null, version?: number }): Promise<void> {
  const k = marcacaoKey(userId, artigoId, m.id)
  const prev = await storeGetRaw('marcacoes', k) as unknown as CachedMarc | undefined
  const val: CachedMarc = {
    key: k, userId, artigoId, id: m.id,
    clientId: opts?.clientId ?? prev?.clientId ?? null,
    version: opts?.version ?? prev?.version ?? 0,
    updatedAt: new Date().toISOString(),
    data: m,
  }
  await storePutRaw('marcacoes', val as unknown as Record<string, unknown>)
}
export async function deleteCachedMarcacao(userId: number, artigoId: number, marcId: number): Promise<void> {
  await storeDeleteRaw('marcacoes', marcacaoKey(userId, artigoId, marcId))
}
export async function getCachedMarcVersion(userId: number, artigoId: number, marcId: number): Promise<{ clientId: string | null, version: number } | null> {
  const raw = await storeGetRaw('marcacoes', marcacaoKey(userId, artigoId, marcId)) as unknown as CachedMarc | undefined
  if (!raw) return null
  return { clientId: raw.clientId, version: raw.version }
}
export async function getCachedNotaVersion(userId: number, artigoId: number, notaId: number): Promise<{ clientId: string | null, version: number } | null> {
  const raw = await storeGetRaw('notas', notaKey(userId, artigoId, notaId)) as unknown as CachedNota | undefined
  if (!raw) return null
  return { clientId: raw.clientId, version: raw.version }
}

// buscas
export async function cacheBusca(userId: number, artigoId: number, q: string, resultados: BuscaResultado[]): Promise<void> {
  const norm = q.trim().toLowerCase()
  await storePutRaw('buscas', { key: `${userId}:${artigoId}:${norm}`, userId, artigoId, q: norm, data: resultados, cachedAt: new Date().toISOString() })
}
export async function loadBusca(userId: number, artigoId: number, q: string): Promise<BuscaResultado[] | null> {
  const norm = q.trim().toLowerCase()
  const raw = await storeGetRaw('buscas', `${userId}:${artigoId}:${norm}`) as { data: BuscaResultado[] } | undefined
  return raw?.data ?? null
}

// extras: sumario, citacoes, historico, etc per artigo
function extraKey(userId: number, artigoId: number, kind: string): string { return `${userId}:${artigoId}:${kind}` }
export async function cacheSumario(userId: number, artigoId: number, sumario: SumarioItem[]): Promise<void> {
  await storePutRaw('extras', { key: extraKey(userId, artigoId, 'sumario'), userId, artigoId, kind: 'sumario', data: sumario })
}
export async function loadSumario(userId: number, artigoId: number): Promise<SumarioItem[] | null> {
  const raw = await storeGetRaw('extras', extraKey(userId, artigoId, 'sumario')) as { data: SumarioItem[] } | undefined
  return raw?.data ?? null
}
export async function cacheCitacoes(userId: number, artigoId: number, citacoes: Citacao[]): Promise<void> {
  await storePutRaw('extras', { key: extraKey(userId, artigoId, 'citacoes'), userId, artigoId, kind: 'citacoes', data: citacoes })
}
export async function loadCitacoes(userId: number, artigoId: number): Promise<Citacao[] | null> {
  const raw = await storeGetRaw('extras', extraKey(userId, artigoId, 'citacoes')) as { data: Citacao[] } | undefined
  return raw?.data ?? null
}
export async function cacheHistorico(userId: number, artigoId: number, hist: HistoricoEvento[]): Promise<void> {
  await storePutRaw('extras', { key: extraKey(userId, artigoId, 'historico'), userId, artigoId, kind: 'historico', data: hist })
}
export async function loadHistorico(userId: number, artigoId: number): Promise<HistoricoEvento[] | null> {
  const raw = await storeGetRaw('extras', extraKey(userId, artigoId, 'historico')) as { data: HistoricoEvento[] } | undefined
  return raw?.data ?? null
}

// for tests isolation helper
export async function __clearUserCache(userId: number): Promise<void> {
  // delete all keys prefixed with userId:
  for (const store of ['artigos','detalhes','camadas','imagens','notas','marcacoes','buscas','extras'] as const) {
    const all = await storeGetAllByPrefixRaw(store, `${userId}:`)
    for (const r of all) await storeDeleteRaw(store, r.key as string)
  }
}
