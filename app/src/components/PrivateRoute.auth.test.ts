import { describe, expect, it, vi } from 'vitest'
import { isPrivateRouteAllowed } from './PrivateRoute'
import { ApiError, setUnauthorizedHandler, api } from '../api'

describe('PrivateRoute - lógica de acesso', () => {
  it('loading true nunca permite', () => {
    expect(isPrivateRouteAllowed(null, true)).toBe(false)
    expect(isPrivateRouteAllowed({ id: 1, nome: 'Ana' }, true)).toBe(false)
  })
  it('sem user e não loading => bloqueado', () => {
    expect(isPrivateRouteAllowed(null, false)).toBe(false)
    expect(isPrivateRouteAllowed(undefined, false)).toBe(false)
  })
  it('com user e não loading => permitido', () => {
    expect(isPrivateRouteAllowed({ id: 1, nome: 'Ana Bagatinii' }, false)).toBe(true)
  })
})

describe('PrivateRoute - interceptor 401 redireciona', () => {
  it('request 401 dispara unauthorizedHandler', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ erro: 'não autenticado' }), { status: 401 }),
    ) as unknown as typeof fetch
    try {
      await api.listarArtigos()
    } catch (e) {
      expect((e as ApiError).status).toBe(401)
    }
    expect(handler).toHaveBeenCalledOnce()
    setUnauthorizedHandler(null)
    globalThis.fetch = originalFetch
  })

  it('200 com authenticated:false não dispara handler (evita loop)', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ authenticated: false }), { status: 200 }),
    ) as unknown as typeof fetch
    const me = await api.me()
    expect(me).toBeNull()
    expect(handler).not.toHaveBeenCalled()
    setUnauthorizedHandler(null)
    globalThis.fetch = originalFetch
  })

  it('401 em /api/health não dispara handler', async () => {
    const handler = vi.fn()
    setUnauthorizedHandler(handler)
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ erro: 'não autenticado' }), { status: 401 }),
    ) as unknown as typeof fetch
    try {
      await api.health()
    } catch {
      // esperado
    }
    expect(handler).not.toHaveBeenCalled()
    setUnauthorizedHandler(null)
    globalThis.fetch = originalFetch
  })
})

describe('AuthContext - sessão persiste via me', () => {
  it('me com cookie válido retorna user', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async (url: string | URL | Request) => {
      const u = String(url)
      if (u.includes('/api/auth/me')) {
        return new Response(JSON.stringify({ id: 1, nome: 'Ana Bagatinii', authenticated: true }), { status: 200 })
      }
      return new Response(JSON.stringify({ authenticated: false }), { status: 200 })
    }) as unknown as typeof fetch
    const me = await api.me()
    expect(me!.id).toBe(1)
    expect(me!.nome).toBe('Ana Bagatinii')
    globalThis.fetch = originalFetch
  })

  it('me sem cookie retorna null e AuthContext deve limpar user', async () => {
    const originalFetch = globalThis.fetch
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ authenticated: false }), { status: 200 }),
    ) as unknown as typeof fetch
    const me = await api.me()
    expect(me).toBeNull()
    globalThis.fetch = originalFetch
  })

  it('logout chama POST com credentials:include', async () => {
    const fetchMock = vi.fn(async () => new Response(null, { status: 204 }))
    const originalFetch = globalThis.fetch
    globalThis.fetch = fetchMock as unknown as typeof fetch
    await api.logout()
    expect(fetchMock).toHaveBeenCalledOnce()
    const call = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    const init = call[1]
    expect(init.method).toBe('POST')
    expect(init.credentials).toBe('include')
    globalThis.fetch = originalFetch
  })
})
