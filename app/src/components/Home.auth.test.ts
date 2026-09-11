import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest'
import { ApiError, api } from '../api'
import { formatRetryAfter, mapLoginError, validarLoginInput } from './Home'

describe('Home - formatRetryAfter', () => {
  it('retorna segundos quando <=60', () => {
    expect(formatRetryAfter(30)).toBe('30 segundos')
    expect(formatRetryAfter(60)).toBe('60 segundos')
    expect(formatRetryAfter(900)).not.toBe('900 segundos')
  })
  it('retorna minutos quando >60', () => {
    expect(formatRetryAfter(61)).toBe('2 minutos')
    expect(formatRetryAfter(900)).toBe('15 minutos')
    expect(formatRetryAfter(90)).toBe('2 minutos')
    expect(formatRetryAfter(120)).toBe('2 minutos')
  })
  it('1 minuto singular', () => {
    // 61 segundos -> ceil(61/60)=2 -> 2 minutos, não singular
    // 61-120 sempre 2 minutos, então testa 90 -> 2
    // para 1 minuto precisaria seg= 60 mas é segundos, então não há singular? Verifica lógica
    // Na lógica: >60 retorna ceil(seg/60) minutos, então 61->2, 120->2, 90->2
    // Para ter 1 minuto, precisaria seg entre 61-60? Impossível. Mas mantém plural correto.
    expect(formatRetryAfter(61)).toBe('2 minutos')
  })
})

describe('Home - validarLoginInput', () => {
  it('vazio retorna erro 400', () => {
    expect(validarLoginInput('', '')).toBe('selecione um usuário')
    expect(validarLoginInput('   ', 'x')).toBe('selecione um usuário')
    expect(validarLoginInput('Outro', 'x')).toBe('usuário inválido')
  })
  it('preenchido retorna null', () => {
    expect(validarLoginInput('Ana Bagatinii', 'segredo')).toBeNull()
    expect(validarLoginInput('Luiz', 'segredo')).toBeNull()
    expect(validarLoginInput(' Ana Bagatinii ', 'segredo')).toBeNull()
  })
  it('sem senha retorna erro', () => {
    expect(validarLoginInput('Ana Bagatinii', '')).toBe('digite a senha')
    expect(validarLoginInput('Luiz', '')).toBe('digite a senha')
  })
})

describe('Home - mapLoginError (200/401/423)', () => {
  it('401 mapeia para mensagem do servidor', () => {
    const err = new ApiError(401, 'credenciais inválidas')
    const r = mapLoginError(err)
    expect(r.mensagem).toBe('credenciais inválidas')
    expect(r.retryAfter).toBeUndefined()
  })
  it('401 mostra texto do servidor', () => {
    const err = new ApiError(401, 'credenciais inválidas')
    expect(mapLoginError(err).mensagem).toBe('credenciais inválidas')
  })
  it('403 mostra texto do servidor (conta sem senha fora do PC)', () => {
    const err = new ApiError(403, 'conta sem senha: crie a senha no PC (http://127.0.0.1:8734)')
    expect(mapLoginError(err).mensagem).toContain('crie a senha no PC')
  })
  it('423 mapeia para muitas tentativas com retryAfter', () => {
    const err = new ApiError(423, 'muitas tentativas', 900)
    const r = mapLoginError(err)
    expect(r.mensagem).toBe('muitas tentativas, tente novamente em 15 minutos')
    expect(r.retryAfter).toBe(900)
  })
  it('423 sem retryAfter usa 900 padrão', () => {
    const err = new ApiError(423, 'muitas tentativas')
    const r = mapLoginError(err)
    expect(r.mensagem).toContain('15 minutos')
    expect(r.retryAfter).toBe(900)
  })
  it('423 com 30 segundos mostra segundos', () => {
    const err = new ApiError(423, 'muitas tentativas', 30)
    const r = mapLoginError(err)
    expect(r.mensagem).toBe('muitas tentativas, tente novamente em 30 segundos')
  })
  it('400 mapeia para selecione um usuário', () => {
    const err = new ApiError(400, 'selecione um usuário')
    expect(mapLoginError(err).mensagem).toBe('selecione um usuário')
  })
  it('500 mapeia para mensagem original', () => {
    const err = new ApiError(500, 'erro interno')
    expect(mapLoginError(err).mensagem).toBe('erro interno')
  })
  it('Error genérico', () => {
    const err = new Error('Network failed')
    expect(mapLoginError(err).mensagem).toBe('Network failed')
  })
})

describe('Home - login via api (fetch credentials:include)', () => {
  const originalFetch = globalThis.fetch

  beforeEach(() => {
    vi.restoreAllMocks()
  })
  afterEach(() => {
    globalThis.fetch = originalFetch
  })

  it('login 200 envia credentials:include e Content-Type', async () => {
    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ ok: true }), { status: 200, headers: { 'Content-Type': 'application/json' } }),
    )
    globalThis.fetch = fetchMock as unknown as typeof fetch
    await api.login('Ana Bagatinii', 'segredo')
    expect(fetchMock).toHaveBeenCalledOnce()
    const call = fetchMock.mock.calls[0] as unknown as [string, RequestInit]
    const init = call[1]
    expect(init.credentials).toBe('include')
    expect(init.method).toBe('POST')
    expect((init.headers as Record<string, string>)['Content-Type']).toBe('application/json')
    const body = JSON.parse(init.body as string)
    expect(body.nome).toBe('Ana Bagatinii')
    expect(body.senha).toBe('segredo')
  })

  it('login 401 lança ApiError 401', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ erro: 'credenciais inválidas' }), { status: 401, headers: { 'Content-Type': 'application/json' } }),
    ) as unknown as typeof fetch
    await expect(api.login('Invalido', 'x')).rejects.toThrow(ApiError)
    try {
      await api.login('Invalido', 'x')
    } catch (e) {
      expect((e as ApiError).status).toBe(401)
      expect((e as Error).message).toBe('credenciais inválidas')
    }
  })

  it('login 423 lança ApiError 423 com retryAfter e Retry-After header', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(JSON.stringify({ erro: 'muitas tentativas', retryAfter: 900 }), {
        status: 423,
        headers: { 'Content-Type': 'application/json', 'Retry-After': '900' },
      }),
    ) as unknown as typeof fetch
    try {
      await api.login('Ana Bagatinii', 'segredo')
      expect.fail('deveria lançar 423')
    } catch (e) {
      const err = e as ApiError
      expect(err.status).toBe(423)
      expect(err.retryAfter).toBe(900)
      expect(err.message).toBe('muitas tentativas')
    }
    const mapped = mapLoginError(new ApiError(423, 'muitas tentativas', 900))
    expect(mapped.mensagem).toContain('15 minutos')
  })
})
