const DB_NAME = 'artigosAnaOffline'
const DB_VERSION = 1
const STORES = [
  'meta',
  'sessions',
  'artigos',
  'detalhes',
  'camadas',
  'imagens',
  'notas',
  'marcacoes',
  'outbox',
  'buscas',
  'extras',
] as const

type StoreName = typeof STORES[number]

let dbPromise: Promise<IDBDatabase | null> | null = null
const mem: Map<string, Map<string, unknown>> = new Map()

function memGetStore(name: string): Map<string, unknown> {
  let m = mem.get(name)
  if (!m) { m = new Map(); mem.set(name, m) }
  return m
}

export function isIndexedDBAvailable(): boolean {
  try {
    return typeof indexedDB !== 'undefined' && !!indexedDB.open
  } catch { return false }
}

function openDB(): Promise<IDBDatabase | null> {
  if (!isIndexedDBAvailable()) return Promise.resolve(null)
  if (dbPromise) return dbPromise
  dbPromise = new Promise((resolve) => {
    try {
      const req = indexedDB.open(DB_NAME, DB_VERSION)
      req.onupgradeneeded = () => {
        const db = req.result
        for (const name of STORES) {
          if (db.objectStoreNames.contains(name)) continue
          if (name === 'outbox') {
            db.createObjectStore(name, { keyPath: 'opId' })
          } else {
            db.createObjectStore(name, { keyPath: 'key' })
          }
        }
      }
      req.onsuccess = () => resolve(req.result)
      req.onerror = () => resolve(null)
      req.onblocked = () => resolve(null)
    } catch {
      resolve(null)
    }
  })
  return dbPromise
}

// force fallback for tests: env sem indexedDB cai no mem automaticamente
async function withDB<T>(fn: (db: IDBDatabase) => Promise<T>, fallback: () => Promise<T> | T): Promise<T> {
  const db = await openDB()
  if (!db) return await fallback()
  try {
    return await fn(db)
  } catch {
    return await fallback()
  }
}

function idbGet(db: IDBDatabase, store: StoreName, key: string): Promise<unknown | undefined> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(store, 'readonly')
    const st = tx.objectStore(store)
    const req = st.get(key)
    req.onsuccess = () => resolve(req.result as unknown)
    req.onerror = () => reject(req.error)
  })
}

function idbPut(db: IDBDatabase, store: StoreName, value: unknown): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(store, 'readwrite')
    const st = tx.objectStore(store)
    const req = st.put(value as never)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

function idbDelete(db: IDBDatabase, store: StoreName, key: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(store, 'readwrite')
    const st = tx.objectStore(store)
    const req = st.delete(key)
    req.onsuccess = () => resolve()
    req.onerror = () => reject(req.error)
  })
}

function idbGetAll(db: IDBDatabase, store: StoreName): Promise<unknown[]> {
  return new Promise((resolve, reject) => {
    const tx = db.transaction(store, 'readonly')
    const st = tx.objectStore(store)
    const req = st.getAll()
    req.onsuccess = () => resolve(req.result as unknown[])
    req.onerror = () => reject(req.error)
  })
}

export async function kvGet(store: StoreName, key: string): Promise<unknown | undefined> {
  return withDB(
    (db) => idbGet(db, store, key).then(v => {
      // unwrap envelope if value stored as {key, value} vs {key, ...}
      if (v && typeof v === 'object' && 'value' in (v as Record<string, unknown>) && Object.keys(v as object).length === 2 && 'key' in (v as Record<string, unknown>)) {
        return (v as { value: unknown }).value
      }
      return v
    }),
    () => {
      const m = memGetStore(store)
      const raw = m.get(key)
      if (raw && typeof raw === 'object' && 'value' in (raw as Record<string, unknown>) && Object.keys(raw as object).length === 2) {
        return (raw as { value: unknown }).value
      }
      return raw
    },
  )
}

export async function kvSet(store: StoreName, key: string, value: unknown): Promise<void> {
  const envelope = { key, value }
  // sessions/artigos etc store direct object with key field; meta uses envelope? We'll support both patterns via envelope for simple stores
  // But for generic we use envelope; for typed caches we use direct put with key field included
  return withDB(
    (db) => idbPut(db, store, envelope),
    () => { memGetStore(store).set(key, envelope); return Promise.resolve() },
  )
}

export async function kvDelete(store: StoreName, key: string): Promise<void> {
  return withDB(
    (db) => idbDelete(db, store, key),
    () => { memGetStore(store).delete(key); return Promise.resolve() },
  )
}

// direct object store helpers for typed caches (where value itself has key prop)
export async function storePutRaw(store: StoreName, value: Record<string, unknown>): Promise<void> {
  const k = value.key as string
  if (!k) throw new Error('storePutRaw requires key')
  return withDB(
    (db) => idbPut(db, store, value),
    () => { memGetStore(store).set(k, value); return Promise.resolve() },
  )
}
export async function storeGetRaw(store: StoreName, key: string): Promise<Record<string, unknown> | undefined> {
  return withDB(
    (db) => idbGet(db, store, key) as Promise<Record<string, unknown> | undefined>,
    () => memGetStore(store).get(key) as Record<string, unknown> | undefined,
  )
}
export async function storeDeleteRaw(store: StoreName, key: string): Promise<void> {
  return withDB(
    (db) => idbDelete(db, store, key),
    () => { memGetStore(store).delete(key); return Promise.resolve() },
  )
}
export async function storeGetAllRaw(store: StoreName): Promise<Record<string, unknown>[]> {
  return withDB(
    (db) => idbGetAll(db, store) as Promise<Record<string, unknown>[]>,
    () => Array.from(memGetStore(store).values()) as Record<string, unknown>[],
  )
}
export async function storeGetAllByPrefixRaw(store: StoreName, prefix: string): Promise<Record<string, unknown>[]> {
  const all = await storeGetAllRaw(store)
  return all.filter(r => typeof r.key === 'string' && (r.key as string).startsWith(prefix))
}

// outbox is separated (keyPath opId)
export async function outboxPut(op: Record<string, unknown>): Promise<void> {
  return withDB(
    (db) => idbPut(db, 'outbox', op),
    () => { memGetStore('outbox').set(op.opId as string, op); return Promise.resolve() },
  )
}
export async function outboxGet(opId: string): Promise<Record<string, unknown> | undefined> {
  return withDB(
    (db) => idbGet(db, 'outbox', opId) as Promise<Record<string, unknown> | undefined>,
    () => memGetStore('outbox').get(opId) as Record<string, unknown> | undefined,
  )
}
export async function outboxDelete(opId: string): Promise<void> {
  return withDB(
    (db) => idbDelete(db, 'outbox', opId),
    () => { memGetStore('outbox').delete(opId); return Promise.resolve() },
  )
}
export async function outboxGetAll(): Promise<Record<string, unknown>[]> {
  return withDB(
    (db) => idbGetAll(db, 'outbox') as Promise<Record<string, unknown>[]>,
    () => Array.from(memGetStore('outbox').values()) as Record<string, unknown>[],
  )
}

// util to clear mem in tests
export function __clearMemory(): void {
  mem.clear()
  dbPromise = null
}

// for tests to force fallback even if indexedDB available
export function __resetDbPromise(): void { dbPromise = null }
