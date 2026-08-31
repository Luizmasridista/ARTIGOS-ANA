import { storeDeleteRaw, storeGetAllRaw, storeGetRaw, storePutRaw, kvGet, kvSet } from './db'

export interface OfflineSession {
  key: string
  userId: number
  id: number
  nome: string
  onlineAt: string
  expiresAt: string
}

const SEVEN_DAYS_MS = 7 * 24 * 60 * 60 * 1000

function keyFor(userId: number): string { return `user:${userId}` }

export async function saveOfflineSession(session: { id: number; nome: string; onlineAt?: string; expiresAt?: string }): Promise<void> {
  const onlineAt = session.onlineAt ?? new Date().toISOString()
  const expiresAt = session.expiresAt ?? new Date(Date.now() + SEVEN_DAYS_MS).toISOString()
  const value: OfflineSession = { key: keyFor(session.id), userId: session.id, id: session.id, nome: session.nome, onlineAt, expiresAt }
  await storePutRaw('sessions', value as unknown as Record<string, unknown>)
}

export async function loadOfflineSession(userId: number): Promise<OfflineSession | null> {
  const raw = await storeGetRaw('sessions', keyFor(userId)) as unknown as OfflineSession | undefined
  if (!raw) return null
  if (new Date(raw.expiresAt).getTime() <= Date.now()) {
    await storeDeleteRaw('sessions', keyFor(userId))
    return null
  }
  return raw
}

export async function loadLatestValidSession(): Promise<OfflineSession | null> {
  const all = await storeGetAllRaw('sessions') as unknown as OfflineSession[]
  const now = Date.now()
  const valid = all.filter(s => new Date(s.expiresAt).getTime() > now)
  if (valid.length === 0) return null
  valid.sort((a, b) => new Date(b.onlineAt).getTime() - new Date(a.onlineAt).getTime())
  return valid[0]
}

export async function clearOfflineSession(userId: number): Promise<void> {
  await storeDeleteRaw('sessions', keyFor(userId))
}

export async function getOrCreateDeviceId(): Promise<string> {
  const existing = await kvGet('meta', 'deviceId') as string | undefined
  if (existing && typeof existing === 'string') {
    try {
      if (typeof localStorage !== 'undefined') localStorage.setItem('ana_device_id', existing)
    } catch {}
    return existing
  }
  const id = genUUID()
  await kvSet('meta', 'deviceId', id)
  try {
    if (typeof localStorage !== 'undefined') localStorage.setItem('ana_device_id', id)
  } catch {}
  return id
}

function genUUID(): string {
  try {
    if (typeof crypto !== 'undefined' && typeof crypto.randomUUID === 'function') return crypto.randomUUID()
  } catch {}
  return 'dev-' + Math.random().toString(36).slice(2) + Date.now().toString(36)
}

export async function getCursor(userId: number): Promise<string | null> {
  const v = await kvGet('meta', `cursor:${userId}`) as string | undefined
  return v ?? null
}
export async function setCursor(userId: number, cursor: string): Promise<void> {
  await kvSet('meta', `cursor:${userId}`, cursor)
}
