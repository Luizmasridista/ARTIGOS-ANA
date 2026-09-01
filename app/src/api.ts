function apiBase(): string {
  try {
    if (typeof window !== 'undefined' && typeof localStorage !== 'undefined') {
      const custom = localStorage.getItem('artigosAnaApiBase')
      if (custom) return custom
    }
  } catch {
    // sem storage (não bloqueia)
  }
  try {
    if (typeof window !== 'undefined' && window.location) {
      const loc = window.location
      // Vite dev (5173) deve falar com backend 8734
      if (loc.port === '5173') return 'http://127.0.0.1:8734'
      // Electron file:// também fala com 8734
      if (loc.protocol === 'file:') return 'http://127.0.0.1:8734'
      // Web: frontend servido pelo próprio backend (mesma origem) — GROK ou localhost:8734
      // usa relativo '' para funcionar em https://xxx.trycloudflare.com e http://localhost:8734
      if (loc.protocol.startsWith('http')) return ''
    }
  } catch {
    // sem window
  }
  return 'http://127.0.0.1:8734'
}

export const API_BASE = apiBase()

export interface ArtigoResumo {
  id: number
  titulo: string
  num_paginas: number
  criado_em: string
}

export interface PaginaInfo {
  numero: number
  largura: number
  altura: number
}

export interface ArtigoDetalhe {
  id: number
  titulo: string
  criado_em: string
  paginas: PaginaInfo[]
}

export interface Palavra {
  texto: string
  x0: number
  y0: number
  x1: number
  y1: number
}

export interface CamadaTexto {
  largura: number
  altura: number
  palavras: Palavra[]
}

export type PalavraBox = [number, number, number, number]

export interface Marcacao {
  id: number
  pagina: number
  tipo: string
  cor: string
  palavras: PalavraBox[]
  texto: string
}

export interface Nota {
  id: number
  pagina: number
  texto: string
  criado_em: string
  marcacao_id?: number
  tags: string[]
  cor: string
}

export interface BuscaResultado {
  pagina: number
  pos: [number, number, number, number]
  trecho: string
}

export interface SumarioItem {
  titulo: string
  pagina: number
  nivel: number
  ordem: number
}

export interface HistoricoEvento {
  id: number
  entidade: string
  entidade_id: number
  acao: string
  dados: unknown
  criado_em: string
}

export interface ExportResultado {
  caminho: string
  nome: string
}

export interface HealthInfo {
  ok: boolean
  versao: string
}

export class ApiError extends Error {
  status: number
  retryAfter?: number

  constructor(status: number, message: string, retryAfter?: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
    if (retryAfter !== undefined) this.retryAfter = retryAfter
  }
}

let unauthorizedHandler: (() => void) | null = null

export function setUnauthorizedHandler(fn: (() => void) | null): void {
  unauthorizedHandler = fn
}

function isAuthPath(path: string): boolean {
  return path.startsWith('/api/auth/') || path === '/api/health' || path === '/health' || path === '/api/info'
}

function montarHeadersAutenticados(headersExistentes?: Record<string, string>): Record<string, string> {
  const h: Record<string, string> = { ...(headersExistentes || {}) }
  try {
    if (typeof navigator !== 'undefined' && typeof window !== 'undefined' && !window.artigosAna) {
      const ua = navigator.userAgent || ''
      const maxTouch = (navigator as unknown as { maxTouchPoints?: number }).maxTouchPoints ?? 0
      const hasTouch = 'ontouchend' in window || maxTouch > 0
      const isIpad = /iPad/.test(ua) || (/Macintosh/.test(ua) && hasTouch && maxTouch > 1)
      if (isIpad && !h['X-Device'] && !h['x-device']) {
        h['X-Device'] = 'iPad'
      }
    }
  } catch {}
  try {
    const isFile = typeof window !== 'undefined' && window.location.protocol === 'file:'
    if (isFile) {
      const token = localStorage.getItem('ana_token')
      if (token && !h['Authorization'] && !h['authorization']) {
        h['Authorization'] = `Bearer ${token}`
      }
    }
  } catch {}
  try {
    if (typeof window !== 'undefined' && typeof localStorage !== 'undefined') {
      const did = localStorage.getItem('ana_device_id') || localStorage.getItem('ana_deviceId') || localStorage.getItem('deviceId')
      if (did && !h['X-Device-Id'] && !h['X-Device-ID'] && !h['x-device-id']) {
        h['X-Device-Id'] = did
      }
    }
  } catch {}
  return h
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const merged: RequestInit = { credentials: 'include', ...init }
  if (!merged.credentials) merged.credentials = 'include'
  {
    const base = (merged.headers as Record<string, string>) || {}
    const h = montarHeadersAutenticados(base)
    if (Object.keys(h).length) merged.headers = h as HeadersInit
  }
  const res = await fetch(`${API_BASE}${path}`, merged)
  if (!res.ok) {
    let message = `Erro ${res.status}`
    let retryAfter: number | undefined
    try {
      const body = (await res.clone().json()) as { erro?: string; retryAfter?: number }
      if (body && typeof body.erro === 'string' && body.erro) message = body.erro
      if (body && typeof body.retryAfter === 'number') retryAfter = body.retryAfter
      else {
        const h = res.headers.get('Retry-After')
        if (h) {
          const n = Number(h)
          if (!Number.isNaN(n)) retryAfter = n
        }
      }
    } catch {
      // sem corpo JSON
      const h = res.headers.get('Retry-After')
      if (h) {
        const n = Number(h)
        if (!Number.isNaN(n)) retryAfter = n
      }
    }
    const err = new ApiError(res.status, message, retryAfter)
    if (res.status === 401 && !isAuthPath(path) && unauthorizedHandler) {
      try {
        unauthorizedHandler()
      } catch {
        // ignora
      }
    }
    throw err
  }
  if (res.status === 204) return undefined as T
  return (await res.json()) as T
}

async function fetchBlob(path: string, signal?: AbortSignal): Promise<Blob> {
  const merged: RequestInit = { credentials: 'include', signal }
  {
    const base = (merged.headers as Record<string, string>) || {}
    const h = montarHeadersAutenticados(base)
    if (Object.keys(h).length) merged.headers = h as HeadersInit
  }
  const res = await fetch(`${API_BASE}${path}`, merged)
  if (!res.ok) {
    let message = `Erro ${res.status}`
    let retryAfter: number | undefined
    try {
      const body = (await res.clone().json()) as { erro?: string; retryAfter?: number }
      if (body && typeof body.erro === 'string' && body.erro) message = body.erro
      if (body && typeof body.retryAfter === 'number') retryAfter = body.retryAfter
      else {
        const h = res.headers.get('Retry-After')
        if (h) {
          const n = Number(h)
          if (!Number.isNaN(n)) retryAfter = n
        }
      }
    } catch {
      const h = res.headers.get('Retry-After')
      if (h) {
        const n = Number(h)
        if (!Number.isNaN(n)) retryAfter = n
      }
    }
    const err = new ApiError(res.status, message, retryAfter)
    if (res.status === 401 && !isAuthPath(path) && unauthorizedHandler) {
      try {
        unauthorizedHandler()
      } catch {}
    }
    throw err
  }
  return await res.blob()
}

export interface MeResponse {
  id: number
  nome: string
  authenticated?: boolean
}

export function imagemUrl(artigoId: number, pagina: number): string {
  return `${API_BASE}/api/artigos/${artigoId}/paginas/${pagina}/imagem`
}

export function downloadExportacaoUrl(artigoId: number): string {
  return `${API_BASE}/api/artigos/${artigoId}/exportar`
}

export interface NovaMarcacao {
  pagina: number
  tipo: string
  cor: string
  palavras: PalavraBox[]
  texto: string
}

export interface NovaNota {
  pagina: number
  texto: string
  marcacao_id?: number
  tags?: string[]
  cor?: string
}

export interface CitacaoOcorrencia {
  pagina: number
  pos: [number, number, number, number] | null
  trecho: string
}

export interface Citacao {
  id: number
  tipo: string
  chave: string
  autor: string
  ano: number | null
  trecho: string
  titulo: string
  texto: string
  url: string
  criado_em: string
  pagina: number
  pos: [number, number, number, number] | null
  ocorrencias: CitacaoOcorrencia[]
}

export const api = {
  health: () => request<HealthInfo>('/api/health'),
  healthFallback: () => request<HealthInfo>('/health'),

  login: (nome: string) => {
    console.log('[api] login', { nome, apiBase: API_BASE, path: '/api/auth/login' })
    return request<{ ok: true; token?: string }>('/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ nome }),
      credentials: 'include',
    })
      .then((r) => {
        console.log('[api] login sucesso', { nome, r })
        // fallback para Electron file:// onde cookie SameSite pode falhar: guarda token no localStorage
        // blindagem captura: só persiste em file: para não expor via XSS/localStorage em web https
        try {
          const isFile = typeof window !== 'undefined' && window.location.protocol === 'file:'
          if (isFile && r && (r as unknown as { token?: string }).token) {
            localStorage.setItem('ana_token', (r as unknown as { token: string }).token)
          }
        } catch {}
        return r as { ok: true }
      })
      .catch((e) => {
        console.error('[api] login erro', { nome, status: e instanceof ApiError ? e.status : undefined, message: e instanceof Error ? e.message : String(e), e })
        throw e
      })
  },
  logout: () => {
    try {
      localStorage.removeItem('ana_token')
    } catch {}
    return request<void>('/api/auth/logout', { method: 'POST', credentials: 'include' })
  },
  me: async (): Promise<MeResponse | null> => {
    const res = (await request<MeResponse & { authenticated?: boolean }>('/api/auth/me', { credentials: 'include' })) as MeResponse & { authenticated?: boolean }
    if (res && (res as unknown as { authenticated?: boolean }).authenticated === false) return null
    if (!res || !res.id || !res.nome) return null
    return res as MeResponse
  },

  listarArtigos: (busca?: string) => {
    const q = busca?.trim() ? `?busca=${encodeURIComponent(busca.trim())}` : ''
    return request<ArtigoResumo[]>(`/api/artigos${q}`)
  },
  criarArtigo: (file: File, titulo?: string) => {
    const form = new FormData()
    form.append('file', file)
    if (titulo) form.append('titulo', titulo)
    return request<ArtigoResumo>('/api/artigos', { method: 'POST', body: form })
  },
  getArtigo: (id: number) => request<ArtigoDetalhe>(`/api/artigos/${id}`),
  deletarArtigo: (id: number) => request<void>(`/api/artigos/${id}`, { method: 'DELETE' }),
  excluirLote: (ids: number[]) =>
    request<void>('/api/artigos/excluir-lote', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ ids }),
    }),

  listarCitacoes: (id: number) => request<Citacao[]>(`/api/artigos/${id}/citacoes`),
  varrerCitacoes: (id: number) =>
    request<Citacao[]>(`/api/artigos/${id}/varrer-citacoes`, { method: 'POST' }),
  enriquecerCitacao: (artigoId: number, citacaoId: number) =>
    request<{ url: string }>(`/api/artigos/${artigoId}/citacoes/${citacaoId}/enriquecer`, {
      method: 'POST',
    }),

  getCamada: (id: number, pagina: number) =>
    request<CamadaTexto>(`/api/artigos/${id}/paginas/${pagina}/camada`),

  getImagem: (id: number, pagina: number, signal?: AbortSignal): Promise<Blob> =>
    fetchBlob(`/api/artigos/${id}/paginas/${pagina}/imagem`, signal),

  listarMarcacoes: (id: number) => request<Marcacao[]>(`/api/artigos/${id}/marcacoes`),
  criarMarcacao: (id: number, body: NovaMarcacao) =>
    request<Marcacao>(`/api/artigos/${id}/marcacoes`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  deletarMarcacao: (id: number, marcacaoId: number) =>
    request<void>(`/api/artigos/${id}/marcacoes/${marcacaoId}`, { method: 'DELETE' }),
  atualizarMarcacaoCor: (id: number, marcacaoId: number, cor: string) =>
    request<void>(`/api/artigos/${id}/marcacoes/${marcacaoId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cor }),
    }),

  listarNotas: (id: number, tag?: string, cor?: string) => {
    const p: string[] = []
    if (tag) p.push(`tag=${encodeURIComponent(tag)}`)
    if (cor) p.push(`cor=${encodeURIComponent(cor)}`)
    const q = p.length ? `?${p.join('&')}` : ''
    return request<Nota[]>(`/api/artigos/${id}/notas${q}`)
  },
  criarNota: (id: number, body: NovaNota) =>
    request<Nota>(`/api/artigos/${id}/notas`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  atualizarNota: (id: number, notaId: number, body: Partial<{ texto: string; tags: string[]; cor: string }>) =>
    request<Nota>(`/api/artigos/${id}/notas/${notaId}`, {
      method: 'PATCH',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    }),
  deletarNota: (id: number, notaId: number) =>
    request<void>(`/api/artigos/${id}/notas/${notaId}`, { method: 'DELETE' }),

  buscarNoArtigo: (id: number, q: string) =>
    request<BuscaResultado[]>(`/api/artigos/${id}/busca?q=${encodeURIComponent(q)}`),

  getSumario: (id: number) => request<SumarioItem[]>(`/api/artigos/${id}/sumario`),

  exportar: (id: number) =>
    request<ExportResultado>(`/api/artigos/${id}/exportar`, { method: 'POST' }),

  listarHistorico: (id: number) =>
    request<HistoricoEvento[]>(`/api/artigos/${id}/historico`),

  // Sync offline-first (novo backend)
  syncPush: (deviceId: string, operations: Array<{ opId: string; clientId: string; entity: string; action: string; data: unknown; baseVersion?: number | null; id?: number | null }>) =>
    request<{ results: Array<{ opId: string; status: 'applied' | 'already_applied' | 'conflict' | 'error'; id?: number; clientId?: string; version?: number; serverData?: unknown; error?: string }> }>(
      '/api/sync',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ deviceId, operations }),
      },
    ),
  syncPull: (cursor: string | null, limit: number) => {
    const qs: string[] = []
    if (cursor) qs.push(`cursor=${encodeURIComponent(cursor)}`)
    if (limit) qs.push(`limit=${limit}`)
    const q = qs.length ? `?${qs.join('&')}` : ''
    return request<{ changes: Array<{ entity: string; id: number; clientId: string; version: number; updatedAt: string; deleted: boolean; data: unknown }>; cursor: string; hasMore: boolean }>(
      `/api/sync/pull${q}`,
    )
  },

  // PROVISORIO debug governança — GET /api/governanca/status retorna {ip, deviceId, autorizado, xDeviceId, xForwardedFor}
  getGovernancaStatus: () =>
    request<{
      ip: string
      deviceId: string
      autorizado: boolean
      xDeviceId: string
      xForwardedFor: string
      enforce: boolean
      ip_mascarado: string
      device_mascarado: string
      via: string
    }>('/api/governanca/status'),
}
