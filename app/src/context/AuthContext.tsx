import { createContext, useCallback, useContext, useEffect, useMemo, useState, type ReactNode } from 'react'
import { api, setUnauthorizedHandler } from '../api'
import { clearOfflineSession, loadLatestValidSession, saveOfflineSession } from '../offline/session'

function isNetworkError(e: unknown): boolean {
  if (e instanceof TypeError) return true
  if (e instanceof DOMException && e.name === 'AbortError') return true
  const msg = e instanceof Error ? e.message : String(e)
  return /Failed to fetch|NetworkError|network|Load failed|AbortError|Timeout|aborted/i.test(msg)
}

const ME_TIMEOUT_MS = 5000

function withTimeout<T>(p: Promise<T>, ms: number): Promise<T> {
  let t: ReturnType<typeof setTimeout> | undefined
  const timeout = new Promise<never>((_, rej) => {
    t = setTimeout(() => rej(new DOMException('Timeout', 'AbortError')), ms)
  })
  return Promise.race([p, timeout]).finally(() => {
    if (t !== undefined) clearTimeout(t)
  }) as Promise<T>
}

export interface AuthUser {
  id: number
  nome: string
}

interface AuthContextValue {
  user: AuthUser | null
  loading: boolean
  login: (nome: string) => Promise<void>
  logout: () => Promise<void>
  refresh: () => Promise<void>
}

const AuthContext = createContext<AuthContextValue | null>(null)

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth deve ser usado dentro de AuthProvider')
  return ctx
}

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<AuthUser | null>(null)
  const [loading, setLoading] = useState(true)

  const refresh = useCallback(async () => {
    try {
      const me = await withTimeout(api.me(), ME_TIMEOUT_MS)
      if (me) {
        setUser({ id: me.id, nome: me.nome })
        await saveOfflineSession({ id: me.id, nome: me.nome })
      } else setUser(null)
    } catch (e) {
      if (isNetworkError(e)) {
        const sess = await loadLatestValidSession()
        if (sess) setUser({ id: sess.id, nome: sess.nome })
        else setUser(null)
      } else setUser(null)
    }
  }, [])

  useEffect(() => {
    let cancelado = false
    const init = async () => {
      try {
        const me = await withTimeout(api.me(), ME_TIMEOUT_MS)
        if (!cancelado) {
          if (me) {
            setUser({ id: me.id, nome: me.nome })
            await saveOfflineSession({ id: me.id, nome: me.nome })
          } else setUser(null)
        }
      } catch (e) {
        if (!cancelado) {
          if (isNetworkError(e)) {
            try {
              const sess = await loadLatestValidSession()
              if (sess && !cancelado) setUser({ id: sess.id, nome: sess.nome })
              else if (!cancelado) setUser(null)
            } catch {
              if (!cancelado) setUser(null)
            }
          } else if (!cancelado) setUser(null)
        }
      } finally {
        if (!cancelado) setLoading(false)
      }
    }
    void init()
    return () => {
      cancelado = true
    }
  }, [])

  useEffect(() => {
    const handler = () => setUser(null)
    setUnauthorizedHandler(handler)
    return () => setUnauthorizedHandler(null)
  }, [])

  const login = useCallback(async (nome: string) => {
    await api.login(nome)
    const me = await api.me()
    if (!me) throw new Error('Falha ao verificar sessão')
    setUser({ id: me.id, nome: me.nome })
    await saveOfflineSession({ id: me.id, nome: me.nome })
  }, [])

  const logout = useCallback(async () => {
    const uid = user?.id ?? null
    try {
      await api.logout()
    } finally {
      if (uid != null) await clearOfflineSession(uid)
      else {
        const sess = await loadLatestValidSession()
        if (sess) await clearOfflineSession(sess.id)
      }
      setUser(null)
    }
  }, [user])

  const value = useMemo<AuthContextValue>(
    () => ({ user, loading, login, logout, refresh }),
    [user, loading, login, logout, refresh],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}
