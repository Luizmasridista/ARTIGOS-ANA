import { outboxDelete, outboxGetAll, outboxPut, storeGetRaw, storeDeleteRaw } from './db'
import { getOrCreateDeviceId, getCursor, setCursor } from './session'
import { deleteCachedMarcacao, deleteCachedNota, upsertCachedMarcacao, upsertCachedNota } from './cache'
import type { Marcacao, Nota } from '../api'

export type SyncEntity = 'nota' | 'marcacao' | 'artigo'
export type SyncAction = 'create' | 'update' | 'delete'
export type OutboxStatus = 'pending' | 'conflict' | 'failed'

export interface OutboxOp {
  opId: string
  clientId: string
  deviceId: string
  userId: number
  artigoId: number
  entity: SyncEntity
  action: SyncAction
  data: unknown
  baseVersion: number | null
  id: number | null
  attempts: number
  status: OutboxStatus
  error?: string
  createdAt: string
  version?: number // local version for tracking
}

export interface SyncStatus {
  syncing: boolean
  pending: number
  conflicts: number
  online: boolean
  lastSyncAt: string | null
  error: string | null
}

const BATCH_LIMIT = 50
const PULL_LIMIT = 100

let inFlight: Promise<void> | null = null
let syncStatus: SyncStatus = { syncing: false, pending: 0, conflicts: 0, online: typeof navigator !== 'undefined' ? navigator.onLine : true, lastSyncAt: null, error: null }
const listeners = new Set<(s: SyncStatus) => void>()
let retryTimer: number | null = null
let intervalTimer: number | null = null
let onlineListenerAttached = false

function notify() {
  for (const cb of listeners) {
    try { cb({ ...syncStatus }) } catch {}
  }
}
function setStatus(patch: Partial<SyncStatus>) {
  syncStatus = { ...syncStatus, ...patch }
  notify()
}

export function subscribeSyncStatus(cb: (s: SyncStatus) => void): () => void {
  listeners.add(cb)
  cb({ ...syncStatus })
  // also kick pending refresh
  void refreshPendingCount()
  return () => { listeners.delete(cb) }
}

async function refreshPendingCount(userId?: number) {
  const all = await outboxGetAll() as unknown as OutboxOp[]
  const pending = all.filter(o => o.status === 'pending' || o.status === 'failed').length // failed still pending? but spec says error not infinite retry -> we count only pending for indicator? Use pending only
  const conflicts = all.filter(o => o.status === 'conflict').length
  const targetPending = userId != null ? all.filter(o => o.userId === userId && o.status === 'pending').length : pending
  setStatus({ pending: targetPending, conflicts })
}

function genUUID(): string {
  try { if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID() } catch {}
  return 'op-' + Math.random().toString(36).slice(2, 10) + '-' + Date.now().toString(36)
}

function isNetworkError(e: unknown): boolean {
  if (e instanceof TypeError) return true
  const msg = e instanceof Error ? e.message : String(e)
  if (/Failed to fetch|NetworkError|network/i.test(msg)) return true
  // ApiError retry? but network will be TypeError before ApiError
  return false
}

function isOnline(): boolean {
  try {
    if (typeof navigator === 'undefined') return true
    if (typeof navigator.onLine !== 'boolean') return true
    return navigator.onLine
  } catch { return true }
}

export async function getPendingCount(userId?: number): Promise<number> {
  const all = await outboxGetAll() as unknown as OutboxOp[]
  if (userId != null) return all.filter(o => o.userId === userId && o.status === 'pending').length
  return all.filter(o => o.status === 'pending').length
}

export async function enqueueOperation(params: {
  userId: number
  artigoId: number
  entity: SyncEntity
  action: SyncAction
  data: unknown
  baseVersion?: number | null
  id?: number | null
  clientId?: string | null
}): Promise<string> {
  const deviceId = await getOrCreateDeviceId()
  const opId = genUUID()
  const clientId = params.clientId ?? genUUID()
  const op: OutboxOp = {
    opId,
    clientId,
    deviceId,
    userId: params.userId,
    artigoId: params.artigoId,
    entity: params.entity,
    action: params.action,
    data: params.data,
    baseVersion: params.baseVersion ?? null,
    id: params.id ?? null,
    attempts: 0,
    status: 'pending',
    createdAt: new Date().toISOString(),
  }
  await outboxPut(op as unknown as Record<string, unknown>)
  await refreshPendingCount(params.userId)
  scheduleSync(300)
  return opId
}

export async function cancelPendingOperationsForTempId(userId: number, artigoId: number, entity: SyncEntity, tempId: number): Promise<void> {
  const all = await outboxGetAll() as unknown as OutboxOp[]
  const toDelete = all.filter(o => o.userId === userId && o.artigoId === artigoId && o.entity === entity && o.id === tempId && o.status === 'pending')
  for (const op of toDelete) {
    await outboxDelete(op.opId)
  }
  if (toDelete.length) await refreshPendingCount(userId)
}

export async function mergePendingCreateData(userId: number, artigoId: number, entity: SyncEntity, tempId: number, patch: Record<string, unknown>): Promise<void> {
  const all = await outboxGetAll() as unknown as OutboxOp[]
  const target = all.find(o => o.userId === userId && o.artigoId === artigoId && o.entity === entity && o.id === tempId && o.action === 'create' && o.status === 'pending')
  if (!target) return
  const mergedData = { ...(target.data as Record<string, unknown> ?? {}), ...patch }
  const updated: OutboxOp = { ...target, data: mergedData }
  await outboxPut(updated as unknown as Record<string, unknown>)
}

function scheduleSync(delayMs: number) {
  if (retryTimer) { clearTimeout(retryTimer); retryTimer = null }
  const g: typeof globalThis & { setTimeout: typeof setTimeout } = globalThis as unknown as typeof globalThis & { setTimeout: typeof setTimeout }
  retryTimer = g.setTimeout(() => { void syncNow() }, delayMs) as unknown as number
}

// Public syncNow: single-flight, batch limité, retry com backoff/jitter, pull com cursor
export async function syncNow(targetUserId?: number): Promise<void> {
  if (inFlight) return inFlight
  // resolve userId for pull: if not given, try to use most recent session user's id via outbox? choose first pending user
  inFlight = doSync(targetUserId).finally(() => { inFlight = null })
  return inFlight
}

async function doSync(targetUserId?: number): Promise<void> {
  if (!isOnline()) {
    setStatus({ online: false, syncing: false })
    return
  }
  setStatus({ syncing: true, online: true, error: null })
  try {
    await pushPending(targetUserId)
    await pullChanges(targetUserId)
    setStatus({ syncing: false, lastSyncAt: new Date().toISOString(), error: null })
    await refreshPendingCount(targetUserId)
    // if pending remains, schedule next batch quickly
    const remaining = await getPendingCount(targetUserId)
    if (remaining > 0) scheduleSync(1200)
  } catch (e) {
    const network = isNetworkError(e)
    if (network) {
      setStatus({ syncing: false, online: false, error: 'Sem conexão' })
      // retry with backoff/jitter: 1s * 2^attempts capped 30s + jitter
      const pendingOps = await outboxGetAll() as unknown as OutboxOp[]
      const attempts = Math.max(0, ...pendingOps.map(o => o.attempts), 0)
      const backoff = Math.min(30000, 1000 * Math.pow(1.8, attempts)) + Math.random() * 800
      scheduleSync(backoff)
    } else {
      setStatus({ syncing: false, error: e instanceof Error ? e.message : String(e) })
    }
  }
}

async function pushPending(targetUserId?: number) {
  const all = await outboxGetAll() as unknown as OutboxOp[]
  let pending = all.filter(o => o.status === 'pending')
  if (targetUserId != null) pending = pending.filter(o => o.userId === targetUserId)
  if (pending.length === 0) return
  pending.sort((a, b) => new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime())
  // não enviar nota com marcacao_id temporário enquanto a criação da marcação estiver pendente
  const pendingMarcacaoTempKeys = new Set(
    pending.filter(o => o.entity === 'marcacao' && o.action === 'create' && typeof o.id === 'number' && (o.id as number) < 0).map(o => `${o.artigoId}:${o.id as number}`),
  )
  const eligible = pending.filter(o => {
    if (o.entity === 'nota') {
      const data = o.data as Record<string, unknown> | null
      const mId = data?.['marcacao_id']
      if (typeof mId === 'number' && mId < 0 && pendingMarcacaoTempKeys.has(`${o.artigoId}:${mId}`)) return false
    }
    return true
  })
  if (eligible.length === 0) return
  const batch = eligible.slice(0, BATCH_LIMIT)
  if (batch.length === 0) return
  const deviceId = await getOrCreateDeviceId()
  const operations = batch.map(o => ({
    opId: o.opId,
    clientId: o.clientId,
    entity: o.entity,
    action: o.action,
    data: o.data,
    baseVersion: o.baseVersion,
    id: o.id,
  }))
  // call backend via api helper to keep credentials logic; import lazily to avoid circular
  const { api } = await import('../api')
  let resp: { results: Array<{ opId: string; status: 'applied' | 'already_applied' | 'conflict' | 'error'; id?: number; clientId?: string; version?: number; serverData?: unknown; error?: string }> }
  try {
    resp = await api.syncPush(deviceId, operations)
  } catch (e) {
    // network failure -> increment attempts but keep pending for retry
    if (isNetworkError(e)) {
      for (const op of batch) {
        const updated: OutboxOp = { ...op, attempts: (op.attempts ?? 0) + 1 }
        await outboxPut(updated as unknown as Record<string, unknown>)
      }
      throw e
    }
    throw e
  }
  // process results per operation
  for (const res of resp.results) {
    const op = batch.find(o => o.opId === res.opId)
    if (!op) continue
    if (res.status === 'applied' || res.status === 'already_applied') {
      await handleApplied(op, res)
      await outboxDelete(op.opId)
    } else if (res.status === 'conflict') {
      const updated: OutboxOp = { ...op, status: 'conflict', error: (res.error as string) ?? 'Conflito: versão desatualizada', attempts: op.attempts }
      // preserve serverData if needed for UI (store in error? keep extra field)
      ;(updated as unknown as Record<string, unknown>)._serverData = res.serverData
      ;(updated as unknown as Record<string, unknown>)._serverVersion = res.version
      await outboxPut(updated as unknown as Record<string, unknown>)
    } else if (res.status === 'error') {
      const updated: OutboxOp = { ...op, status: 'failed', error: (res.error as string) ?? 'Erro ao sincronizar', attempts: op.attempts }
      await outboxPut(updated as unknown as Record<string, unknown>)
    }
  }
  await refreshPendingCount(targetUserId)
}

async function handleApplied(op: OutboxOp, res: { id?: number; clientId?: string; version?: number; serverData?: unknown }) {
  const serverId = res.id ?? (res.serverData as { id?: number } | undefined)?.id ?? null
  const serverVersion = res.version ?? (res.serverData as { version?: number } | undefined)?.version ?? op.baseVersion ?? 0
  const serverClientId = res.clientId ?? op.clientId
  // For create operations with temp negative id, need to remap
  const isCreate = op.action === 'create'
  const oldTempId = op.id
  const newId = serverId != null ? serverId : oldTempId
  if (op.entity === 'marcacao') {
    if (isCreate && oldTempId != null && oldTempId < 0 && newId != null && newId !== oldTempId) {
      // remove old temp entry and upsert new
      await storeDeleteRaw('marcacoes', `${op.userId}:${op.artigoId}:${oldTempId}`)
      const serverData = res.serverData as Marcacao | undefined
      const marc: Marcacao = (serverData && typeof serverData === 'object' && 'id' in (serverData as unknown as Record<string, unknown>))
        ? (serverData as Marcacao)
        : { id: newId as number, pagina: (op.data as unknown as { pagina?: number })?.pagina ?? 1, tipo: 'highlight', cor: (op.data as unknown as { cor?: string })?.cor ?? '#FFE03B', palavras: (op.data as unknown as { palavras?: [number,number,number,number][] })?.palavras ?? [], texto: (op.data as unknown as { texto?: string })?.texto ?? '' }
      await upsertCachedMarcacao(op.userId, op.artigoId, marc, { clientId: serverClientId, version: serverVersion ?? 0 })

      // remap notas that reference old temp marcacao_id
      const pendingForRemap = await outboxGetAll() as unknown as OutboxOp[]
      for (const p of pendingForRemap) {
        if (p.entity === 'nota' && p.status === 'pending' && p.userId === op.userId && p.artigoId === op.artigoId) {
          const data = p.data as Record<string, unknown> | null
          if (data && data['marcacao_id'] === oldTempId) {
            const updated = { ...p, data: { ...data, marcacao_id: newId } }
            await outboxPut(updated as unknown as Record<string, unknown>)
          }
        }
      }
      // also fix cached notas already with temp reference
      const notasToFix = await import('./cache')
      const notas = await notasToFix.loadNotas(op.userId, op.artigoId)
      for (const n of notas) {
        if (n.marcacao_id === oldTempId) {
          const fixed: Nota = { ...n, marcacao_id: newId as number }
          await notasToFix.upsertCachedNota(op.userId, op.artigoId, fixed, { clientId: n.id ? null : undefined })
        }
      }
    } else {
      // update/delete or create already with server id
      if (op.action === 'delete' && newId != null) {
        await deleteCachedMarcacao(op.userId, op.artigoId, newId)
      } else if (serverId != null) {
        const serverData = res.serverData as Marcacao | undefined
        if (serverData) {
          await upsertCachedMarcacao(op.userId, op.artigoId, serverData, { clientId: serverClientId, version: serverVersion ?? 0 })
        } else if (op.action === 'update') {
          // data contains cor patch; apply locally
          const existingRaw = await storeGetRaw('marcacoes', `${op.userId}:${op.artigoId}:${newId}`) as { data: Marcacao } | undefined
          if (existingRaw) {
            const patched = { ...existingRaw.data, ...(op.data as unknown as Record<string, unknown>) } as Marcacao
            await upsertCachedMarcacao(op.userId, op.artigoId, patched, { clientId: serverClientId, version: serverVersion ?? 0 })
          }
        }
      }
    }
  } else if (op.entity === 'nota') {
    if (isCreate && oldTempId != null && oldTempId < 0 && newId != null && newId !== oldTempId) {
      await storeDeleteRaw('notas', `${op.userId}:${op.artigoId}:${oldTempId}`)
      const serverData = res.serverData as Nota | undefined
      const nota: Nota = (serverData && typeof serverData === 'object' && 'id' in (serverData as unknown as Record<string, unknown>))
        ? (serverData as Nota)
        : { id: newId as number, pagina: (op.data as unknown as { pagina?: number })?.pagina ?? 1, texto: (op.data as unknown as { texto?: string })?.texto ?? '', criado_em: new Date().toISOString(), tags: (op.data as unknown as { tags?: string[] })?.tags ?? [], cor: (op.data as unknown as { cor?: string })?.cor ?? '#FFEB3B', marcacao_id: (op.data as unknown as { marcacao_id?: number })?.marcacao_id }
      await upsertCachedNota(op.userId, op.artigoId, nota, { clientId: serverClientId, version: serverVersion ?? 0 })
    } else {
      if (op.action === 'delete' && newId != null) {
        await deleteCachedNota(op.userId, op.artigoId, newId)
      } else if (serverId != null) {
        const serverData = res.serverData as Nota | undefined
        if (serverData) await upsertCachedNota(op.userId, op.artigoId, serverData, { clientId: serverClientId, version: serverVersion ?? 0 })
      }
    }
  }
}

async function pullChanges(targetUserId?: number) {
  // need to know userId for cursor; if not given, infer from valid session or from outbox's first pending?
  let userId = targetUserId ?? null
  if (userId == null) {
    const allOps = await outboxGetAll() as unknown as OutboxOp[]
    if (allOps.length > 0) userId = allOps[0].userId
    else {
      // try load latest session
      const { loadLatestValidSession } = await import('./session')
      const sess = await loadLatestValidSession()
      if (sess) userId = sess.userId
      else return
    }
  }
  let cursor = await getCursor(userId)
  let hasMore = true
  let loops = 0
  const maxLoops = 5
  while (hasMore && loops < maxLoops) {
    loops++
    const { api } = await import('../api')
    let resp: { changes: Array<{ entity: string; id: number; clientId: string; version: number; updatedAt: string; deleted: boolean; data: unknown }>; cursor: string; hasMore: boolean }
    try {
      resp = await api.syncPull(cursor, PULL_LIMIT)
    } catch (e) {
      if (isNetworkError(e)) throw e
      // if 401 etc, stop
      return
    }
    // apply changes without overwriting local pending items
    const pendingAll = await outboxGetAll() as unknown as OutboxOp[]
    const pendingSet = new Set(pendingAll.filter(o => o.status === 'pending' && o.userId === userId).map(o => `${o.entity}:${o.clientId ?? o.id}`))
    // also track by id
    const pendingIdSet = new Set(pendingAll.filter(o => o.status === 'pending' && o.userId === userId && o.id != null).map(o => `${o.entity}:${o.id}`))

    for (const ch of resp.changes) {
      const keyByClient = `${ch.entity}:${ch.clientId}`
      const keyById = `${ch.entity}:${ch.id}`
      if (pendingSet.has(keyByClient) || pendingIdSet.has(keyById)) {
        // skip overwriting local pending item
        continue
      }
      if (ch.entity === 'nota') {
        if (ch.deleted) {
          const artigoId = await resolveArtigoId(userId, ch)
          if (artigoId) await deleteCachedNota(userId, artigoId, ch.id)
          else {
            // fallback: try delete from any artigoId by scanning
            const { storeGetAllRaw, storeDeleteRaw } = await import('./db')
            const all = await storeGetAllRaw('notas') as unknown as Array<{ key:string, userId:number, id:number }>
            for (const r of all.filter(x=> x.userId===userId && x.id===ch.id)) await storeDeleteRaw('notas', r.key)
          }
        } else {
          const nota = ch.data as Nota
          const n: Nota = nota && typeof nota === 'object' && 'id' in (nota as unknown as Record<string, unknown>) ? nota as Nota : { id: ch.id, pagina: 1, texto: String(ch.data ?? ''), criado_em: ch.updatedAt, tags: [], cor: '#FFEB3B' }
          // ensure id matches
          if (n.id == null) (n as Nota).id = ch.id
          const artigoId = await resolveArtigoId(userId, { ...ch, data: n } as never)
          await upsertCachedNota(userId, artigoId || extractArtigoId({ data: n }), n, { clientId: ch.clientId, version: ch.version })
        }
      } else if (ch.entity === 'marcacao') {
        if (ch.deleted) {
          const artigoId = await resolveArtigoId(userId, ch)
          if (artigoId) await deleteCachedMarcacao(userId, artigoId, ch.id)
          else {
            const { storeGetAllRaw, storeDeleteRaw } = await import('./db')
            const all = await storeGetAllRaw('marcacoes') as unknown as Array<{ key:string, userId:number, id:number }>
            for (const r of all.filter(x=> x.userId===userId && x.id===ch.id)) await storeDeleteRaw('marcacoes', r.key)
          }
        } else {
          const marc = ch.data as Marcacao
          const m: Marcacao = marc && typeof marc === 'object' && 'id' in (marc as unknown as Record<string, unknown>) ? marc as Marcacao : { id: ch.id, pagina: 1, tipo: 'highlight', cor: '#FFE03B', palavras: [], texto: '' }
          if (m.id == null) (m as Marcacao).id = ch.id
          const artigoId = await resolveArtigoId(userId, { ...ch, data: m } as never)
          await upsertCachedMarcacao(userId, artigoId || extractArtigoId({ data: m }), m, { clientId: ch.clientId, version: ch.version })
        }
      }
      // artigo entity not yet handled (future)
    }
    cursor = resp.cursor
    hasMore = resp.hasMore
    if (cursor) await setCursor(userId, cursor)
    if (!hasMore) break
  }
}

async function resolveArtigoId(userId: number, ch: { id: number; entity: string; data: unknown }): Promise<number> {
  const d = ch.data as Record<string, unknown> | null
  if (d) {
    if (typeof d.artigo_id === 'number') return d.artigo_id
    if (typeof d.artigoId === 'number') return d.artigoId
    if (typeof d.artigo === 'number') return d.artigo
  }
  // deleted tombstone may have no data: search existing cache for this id
  try {
    const storeName = ch.entity === 'nota' ? 'notas' : ch.entity === 'marcacao' ? 'marcacoes' : null
    if (storeName) {
      const { storeGetAllRaw } = await import('./db')
      const all = await storeGetAllRaw(storeName as never) as unknown as Array<{ userId:number, artigoId:number, id:number }>
      const found = all.find(r => r.userId === userId && r.id === ch.id)
      if (found && typeof found.artigoId === 'number') return found.artigoId
    }
  } catch {}
  return 0
}
function extractArtigoId(ch: { data: unknown }): number {
  const d = ch.data as Record<string, unknown> | null
  if (d && typeof d.artigo_id === 'number') return d.artigo_id
  if (d && typeof d.artigoId === 'number') return d.artigoId
  return 0
}

export function startSyncScheduler(userId?: number) {
  if (typeof window === 'undefined') return () => {}
  if (onlineListenerAttached) return () => {}
  const onOnline = () => { setStatus({ online: true, error: null }); void syncNow(userId) }
  const onOffline = () => setStatus({ online: false })
  const onVisibility = () => { if (document.visibilityState === 'visible') void syncNow(userId) }
  window.addEventListener('online', onOnline)
  window.addEventListener('offline', onOffline)
  document.addEventListener('visibilitychange', onVisibility)
  onlineListenerAttached = true
  const gi = globalThis as unknown as { setInterval: typeof setInterval }
  intervalTimer = gi.setInterval(() => { if (isOnline()) void syncNow(userId) }, 45000) as unknown as number
  return () => {
    window.removeEventListener('online', onOnline)
    window.removeEventListener('offline', onOffline)
    document.removeEventListener('visibilitychange', onVisibility)
    if (intervalTimer) { clearInterval(intervalTimer); intervalTimer = null }
    onlineListenerAttached = false
  }
}

export function stopSyncScheduler() {
  if (intervalTimer) { clearInterval(intervalTimer); intervalTimer = null }
}

export function __resetSyncState() {
  inFlight = null
  syncStatus = { syncing: false, pending: 0, conflicts: 0, online: true, lastSyncAt: null, error: null }
  listeners.clear()
  if (retryTimer) { clearTimeout(retryTimer); retryTimer = null }
  stopSyncScheduler()
  onlineListenerAttached = false
}

export function hasPendingConflict(userId?: number): Promise<boolean> {
  return outboxGetAll().then(all => {
    const ops = all as unknown as OutboxOp[]
    return ops.some(o => o.status === 'conflict' && (userId == null || o.userId === userId))
  })
}
