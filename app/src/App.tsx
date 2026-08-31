import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import './styles/ipad-preview.css'
import {
  api,
  type ArtigoDetalhe,
  type ArtigoResumo,
  type BuscaResultado,
  type Citacao,
  type ExportResultado,
  type HistoricoEvento,
  type Marcacao,
  type Nota,
  type PalavraBox,
  type SumarioItem,
} from './api'
import { subscribeSyncStatus, startSyncScheduler, getPendingCount, enqueueOperation, syncNow, cancelPendingOperationsForTempId, mergePendingCreateData } from './offline/sync'
import type { SyncStatus } from './offline/sync'
import { cacheArtigoDetalhe, loadArtigoDetalhe, cacheMarcacoes, loadMarcacoes, cacheNotas, loadNotas, upsertCachedMarcacao, deleteCachedMarcacao, upsertCachedNota, deleteCachedNota, cacheSumario, loadSumario, cacheCitacoes, loadCitacoes, cacheHistorico, loadHistorico, cacheBusca, loadBusca, getCachedMarcVersion, getCachedNotaVersion } from './offline/cache'

function isNetworkErrorApp(e: unknown): boolean {
  if (e instanceof TypeError) return true
  const msg = e instanceof Error ? e.message : String(e)
  return /Failed to fetch|NetworkError|network|Load failed/i.test(msg)
}
function genTempId(): number {
  return -(Date.now() + Math.floor(Math.random() * 10000))
}
function genUUID(): string {
  try { if (typeof crypto !== 'undefined' && crypto.randomUUID) return crypto.randomUUID() } catch {}
  return 'tmp-' + Math.random().toString(36).slice(2, 10) + Date.now().toString(36)
}
import { aplicarTema, temaInicial, type Tema } from './tema'
import { AuthProvider, useAuth } from './context/AuthContext'
import { BotaoTema } from './components/BotaoTema'
import { Home } from './components/Home'
import {
  IconeBusca,
  IconeDownload,
  IconeFechar,
  IconeNota,
  IconeSetaDireita,
  IconeSetaEsquerda,
  IconeVoltar,
} from './components/Icones'
import { PageView, type BuscaFoco, type FonteFoco } from './components/PageView'
import type { AbaPainel } from './components/PainelLateral'
import { IlhaNota } from './components/IlhaNota'

const Biblioteca = lazy(() => import('./components/Biblioteca').then((m) => ({ default: m.Biblioteca })))
const PainelLateral = lazy(() => import('./components/PainelLateral').then((m) => ({ default: m.PainelLateral })))
const IpadLivePreview = lazy(() => import('./components/IpadLivePreview'))

export function getRotaApp(
  user: { id: number; nome: string } | null,
  authLoading: boolean,
  artigo: { id: number } | null,
): 'loading' | 'home' | 'biblioteca' | 'leitor' {
  if (authLoading) return 'loading'
  if (!user) return 'home'
  if (artigo) return 'leitor'
  return 'biblioteca'
}

interface ExportEstado {
  fase: 'exportando' | 'pronto' | 'erro'
  resultado: ExportResultado | null
  erro: string | null
}

export default function App() {
  return (
    <AuthProvider>
      <AppInner />
    </AuthProvider>
  )
}

function AppInner() {
  // live preview iPad (dev only) — /__ipad-preview ou ?ipadPreview=1 (verificação estrita para não colidir com params normais)
  const isIpadPreview = (() => {
    if (typeof window === 'undefined') return false
    try {
      const url = new URL(window.location.href)
      if (url.pathname === '/__ipad-preview' || url.pathname === '/__ipad-preview/') return true
      if (url.searchParams.has('ipadPreview') || url.searchParams.has('__ipad-preview')) return true
      return false
    } catch {
      return window.location.pathname === '/__ipad-preview' || window.location.search.includes('ipadPreview=')
    }
  })()
  if (isIpadPreview) {
    return (
      <Suspense fallback={<div className="home-carregando">Carregando preview…</div>}>
        <IpadLivePreview />
      </Suspense>
    )
  }

  const { user, loading: authLoading, logout } = useAuth()
  const [tema, setTema] = useState<Tema>(temaInicial)
  const [offline, setOffline] = useState(false)
  const [syncStatus, setSyncStatus] = useState<SyncStatus>({ syncing: false, pending: 0, conflicts: 0, online: true, lastSyncAt: null, error: null })
  const [artigo, setArtigo] = useState<ArtigoResumo | null>(null)

  useEffect(() => {
    aplicarTema(tema)
  }, [tema])

  useEffect(() => {
    let cancelado = false
    const checar = () => {
      api
        .health()
        .then(() => {
          if (!cancelado) setOffline(false)
        })
        .catch(() => {
          if (!cancelado) setOffline(true)
        })
    }
    checar()
    const timer = window.setInterval(checar, 8000)
    return () => {
      cancelado = true
      window.clearInterval(timer)
    }
  }, [])

  useEffect(() => {
    const unsub = subscribeSyncStatus((s) => setSyncStatus(s))
    return unsub
  }, [])

  useEffect(() => {
    if (!user) return
    const stop = startSyncScheduler(user.id)
    // refresh pending count on mount
    void getPendingCount(user.id).then(p => setSyncStatus(s => ({ ...s, pending: p })))
    return () => { stop() }
  }, [user?.id])

  // limpa artigo ao deslogar
  useEffect(() => {
    if (!user) setArtigo(null)
  }, [user])

  if (authLoading) {
    return (
      <div className="app">
        <header className="header">
          <div className="janela-controles">
            <button
              type="button"
              className="janela-btn janela-fechar"
              title="Fechar"
              aria-label="Fechar"
              onClick={() => void window.artigosAna?.janela.fechar()}
            >
              <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
                <path d="M1 1l8 8M9 1l-8 8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
              </svg>
            </button>
            <button
              type="button"
              className="janela-btn janela-minimizar"
              title="Minimizar"
              aria-label="Minimizar"
              onClick={() => void window.artigosAna?.janela.minimizar()}
            >
              <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
                <path d="M1 8h8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
              </svg>
            </button>
            <button
              type="button"
              className="janela-btn janela-maximizar"
              title="Maximizar / restaurar"
              aria-label="Maximizar ou restaurar"
              onClick={() => void window.artigosAna?.janela.alternarMaximizar()}
            >
              <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
                <rect x="1.2" y="1.2" width="7.6" height="7.6" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
              </svg>
            </button>
          </div>
          <div className="brand">Artigos Ana</div>
          <div className="header-spacer" />
          <BotaoTema tema={tema} onAlternar={() => setTema((t) => (t === 'dark' ? 'light' : 'dark'))} />
        </header>
        <div className="home-carregando" role="status" aria-live="polite">
          Carregando…
        </div>
      </div>
    )
  }

  return (
    <div className="app">
      <header className="header">
        <div className="janela-controles">
          <button
            type="button"
            className="janela-btn janela-fechar"
            title="Fechar"
            aria-label="Fechar"
            onClick={() => void window.artigosAna?.janela.fechar()}
          >
            <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
              <path d="M1 1l8 8M9 1l-8 8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
            </svg>
          </button>
          <button
            type="button"
            className="janela-btn janela-minimizar"
            title="Minimizar"
            aria-label="Minimizar"
            onClick={() => void window.artigosAna?.janela.minimizar()}
          >
            <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
              <path d="M1 8h8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
            </svg>
          </button>
          <button
            type="button"
            className="janela-btn janela-maximizar"
            title="Maximizar / restaurar"
            aria-label="Maximizar ou restaurar"
            onClick={() => void window.artigosAna?.janela.alternarMaximizar()}
          >
            <svg width="9" height="9" viewBox="0 0 10 10" aria-hidden="true">
              <rect x="1.2" y="1.2" width="7.6" height="7.6" rx="1" fill="none" stroke="currentColor" strokeWidth="1.4" />
            </svg>
          </button>
        </div>
        <div className="brand">Artigos Ana</div>
        <div className="header-spacer" />
        {user && (
          <div className="header-usuario">
            <span className="header-nome" title={user.nome}>
              {user.nome}
            </span>
            <button
              type="button"
              className="btn btn-sm"
              onClick={() => void logout()}
              aria-label="Sair"
            >
              Sair
            </button>
          </div>
        )}
        <BotaoTema tema={tema} onAlternar={() => setTema((t) => (t === 'dark' ? 'light' : 'dark'))} />
      </header>

      {!user ? (
        <Home />
      ) : artigo ? (
        <Leitor artigo={artigo} onVoltar={() => setArtigo(null)} />
      ) : (
        <Suspense fallback={<div className="home-carregando" role="status" aria-live="polite">Carregando biblioteca…</div>}>
          <Biblioteca onAbrirArtigo={setArtigo} />
        </Suspense>
      )}

      {(() => {
        const isOffline = offline || !syncStatus.online
        const pending = syncStatus.pending
        const hasConflict = syncStatus.conflicts > 0
        const hasError = !!syncStatus.error
        let texto: string | null = null
        let cls = 'banner-offline'
        if (isOffline) {
          texto = pending > 0 ? `Sem conexão — ${pending} ${pending === 1 ? 'alteração salva' : 'alterações salvas'} neste aparelho` : 'Sem conexão — alterações salvas neste aparelho'
        } else if (hasConflict) {
          texto = `${syncStatus.conflicts} conflito${syncStatus.conflicts > 1 ? 's' : ''} — toque para resolver ou aguarde`
          cls = 'banner-offline banner-offline--conflict'
        } else if (syncStatus.syncing && pending > 0) {
          texto = `Sincronizando ${pending} ${pending === 1 ? 'alteração' : 'alterações'}…`
        } else if (pending > 0) {
          texto = `${pending} ${pending === 1 ? 'alteração pendente' : 'alterações pendentes'}`
        } else if (hasError) {
          texto = syncStatus.error
        }
        if (isOffline || pending > 0 || hasConflict || hasError || syncStatus.syncing) {
          // mostra sempre que houver estado relevante; offline tem prioridade
          if (!texto && isOffline) texto = 'Sem conexão — alterações salvas neste aparelho'
          if (!texto) return null
          return (
            <div className={cls} role="status" aria-live="polite">
              <span className="banner-offline-ponto" style={{ background: hasConflict || hasError ? '#e67e22' : isOffline ? 'var(--danger)' : '#2ecc71' }} />
              {texto}
            </div>
          )
        }
        if (offline) {
          return (
            <div className="banner-offline" role="status">
              <span className="banner-offline-ponto" />
              Backend offline — reconectando…
            </div>
          )
        }
        return null
      })()}
    </div>
  )
}

interface LeitorProps {
  artigo: ArtigoResumo
  onVoltar: () => void
}

function Leitor({ artigo, onVoltar }: LeitorProps) {
  const [detalhe, setDetalhe] = useState<ArtigoDetalhe | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  const [paginaAtual, setPaginaAtual] = useState(1)
  const [marcacoes, setMarcacoes] = useState<Marcacao[]>([])
  const [notas, setNotas] = useState<Nota[]>([])
  const [aba, setAba] = useState<AbaPainel>('notas')
  const [historico, setHistorico] = useState<HistoricoEvento[] | null>(null)
  const [historicoErro, setHistoricoErro] = useState(false)
  const [historicoNonce, setHistoricoNonce] = useState(0)
  const [citacoes, setCitacoes] = useState<Citacao[] | null>(null)
  const [citacoesCarregando, setCitacoesCarregando] = useState(false)
  const [citacoesErro, setCitacoesErro] = useState<string | null>(null)
  const [enriquecendoId, setEnriquecendoId] = useState<number | null>(null)
  const varrerJaTentado = useRef(false)
  const [prefill, setPrefill] = useState<{ texto: string; pagina: number; marcacaoId?: number } | null>(null)
  const [prefillNonce, setPrefillNonce] = useState(0)
  const [notaFoco, setNotaFoco] = useState<{ id: number; nonce: number } | null>(null)
  const [painelAberto, setPainelAberto] = useState(true)
  const [removendoNotaId, setRemovendoNotaId] = useState<number | null>(null)
  const [exportEstado, setExportEstado] = useState<ExportEstado | null>(null)
  const [aviso, setAviso] = useState<string | null>(null)
  const paginaRefs = useRef(new Map<number, HTMLDivElement>())
  const avisoTimer = useRef<number | null>(null)
  const [fonteFoco, setFonteFoco] = useState<FonteFoco | null>(null)
  const ocorrenciaIdxRef = useRef<Map<number, number>>(new Map())

  // Busca no PDF
  const [buscaAberta, setBuscaAberta] = useState(false)
  const [buscaQuery, setBuscaQuery] = useState('')
  const [buscaResultados, setBuscaResultados] = useState<BuscaResultado[]>([])
  const [buscaIdx, setBuscaIdx] = useState(0)
  const [buscaCarregando, setBuscaCarregando] = useState(false)
  const [buscaErro, setBuscaErro] = useState<string | null>(null)
  const [buscaFoco, setBuscaFoco] = useState<BuscaFoco | null>(null)
  const buscaInputRef = useRef<HTMLInputElement>(null)

  // Sumario
  const [sumario, setSumario] = useState<SumarioItem[] | null>(null)
  const [sumarioCarregando, setSumarioCarregando] = useState(false)
  const [sumarioErro, setSumarioErro] = useState<string | null>(null)

  const mostrarAviso = useCallback((mensagem: string) => {
    setAviso(mensagem)
    if (avisoTimer.current) window.clearTimeout(avisoTimer.current)
    avisoTimer.current = window.setTimeout(() => setAviso(null), 4000)
  }, [])

  const invalidarHistorico = useCallback(() => {
    setHistorico(null)
    setHistoricoNonce((n) => n + 1)
  }, [])

  const { user: leitorUser } = useAuth()

  useEffect(() => {
    let cancelado = false
    const load = async () => {
      try {
        const d = await api.getArtigo(artigo.id)
        if (!cancelado) {
          setDetalhe(d)
          setErro(null)
          if (leitorUser?.id) { try { await cacheArtigoDetalhe(leitorUser.id, d) } catch {} }
        }
      } catch (e) {
        if (!cancelado) {
          if (isNetworkErrorApp(e) && leitorUser?.id) {
            const cached = await loadArtigoDetalhe(leitorUser.id, artigo.id)
            if (cached) { setDetalhe(cached); setErro(null) }
            else setErro(e instanceof Error ? e.message : 'Falha ao abrir o artigo')
          } else {
            setErro(e instanceof Error ? e.message : 'Falha ao abrir o artigo')
          }
        }
      }
    }
    void load()
    return () => { cancelado = true }
  }, [artigo.id, leitorUser?.id])

  useEffect(() => {
    let cancelado = false
    const load = async () => {
      // marcações e notas: tenta rede, fallback cache
      try {
        const m = await api.listarMarcacoes(artigo.id)
        if (!cancelado) {
          setMarcacoes(m)
          if (leitorUser?.id) { try { await cacheMarcacoes(leitorUser.id, artigo.id, m) } catch {} }
        }
      } catch (e) {
        if (isNetworkErrorApp(e) && leitorUser?.id) {
          const cached = await loadMarcacoes(leitorUser.id, artigo.id)
          if (!cancelado && cached.length > 0) setMarcacoes(cached)
        }
      }
      try {
        const n = await api.listarNotas(artigo.id)
        if (!cancelado) {
          setNotas(n)
          if (leitorUser?.id) { try { await cacheNotas(leitorUser.id, artigo.id, n) } catch {} }
        }
      } catch (e) {
        if (isNetworkErrorApp(e) && leitorUser?.id) {
          const cached = await loadNotas(leitorUser.id, artigo.id)
          if (!cancelado && cached.length > 0) setNotas(cached)
        }
      }
    }
    void load()
    return () => { cancelado = true }
  }, [artigo.id, leitorUser?.id])

  // sumario fetch com cache
  useEffect(() => {
    let cancelado = false
    setSumarioCarregando(true)
    setSumarioErro(null)
    const doFetch = async () => {
      try {
        const s = await api.getSumario(artigo.id)
        if (!cancelado) {
          setSumario(s)
          setSumarioCarregando(false)
          if (leitorUser?.id) { try { await cacheSumario(leitorUser.id, artigo.id, s) } catch {} }
        }
      } catch (e) {
        if (!cancelado) {
          if (isNetworkErrorApp(e) && leitorUser?.id) {
            const cached = await loadSumario(leitorUser.id, artigo.id)
            if (cached) {
              setSumario(cached)
              setSumarioErro(null)
              setSumarioCarregando(false)
              return
            }
          }
          setSumarioErro(e instanceof Error ? e.message : 'Falha ao carregar sumário')
          setSumario([])
          setSumarioCarregando(false)
        }
      }
    }
    void doFetch()
    return () => { cancelado = true }
  }, [artigo.id, leitorUser?.id])

  // busca handlers
  const abrirBusca = useCallback(() => {
    setBuscaAberta(true)
    requestAnimationFrame(() => buscaInputRef.current?.focus())
  }, [])
  const fecharBusca = useCallback(() => {
    setBuscaAberta(false)
    setBuscaQuery('')
    setBuscaResultados([])
    setBuscaIdx(0)
    setBuscaFoco(null)
    setBuscaErro(null)
  }, [])

  const limparBuscaFoco = useCallback(() => setBuscaFoco(null), [])
  const irParaBusca = useCallback(
    (idx: number) => {
      if (buscaResultados.length === 0) return
      const alvoIdx = ((idx % buscaResultados.length) + buscaResultados.length) % buscaResultados.length
      setBuscaIdx(alvoIdx)
      const alvo = buscaResultados[alvoIdx]
      setBuscaFoco({ pagina: alvo.pagina, pos: alvo.pos, nonce: Date.now() })
      setPaginaAtual(alvo.pagina)
      requestAnimationFrame(() => {
        paginaRefs.current.get(alvo.pagina)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      })
    },
    [buscaResultados],
  )

  // atalho Ctrl+K e Esc
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault()
        if (buscaAberta) {
          buscaInputRef.current?.focus()
        } else {
          abrirBusca()
        }
      }
      if (e.key === 'Escape' && buscaAberta) {
        fecharBusca()
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [buscaAberta, abrirBusca, fecharBusca])

  // debounce busca query -> fetch com cache offline
  useEffect(() => {
    const q = buscaQuery.trim()
    if (!buscaAberta) return
    if (q.length === 0) {
      setBuscaResultados([])
      setBuscaIdx(0)
      setBuscaFoco(null)
      setBuscaErro(null)
      setBuscaCarregando(false)
      return
    }
    if (q.length < 2) {
      setBuscaErro('Digite pelo menos 2 caracteres')
      setBuscaResultados([])
      setBuscaFoco(null)
      return
    }
    const t = window.setTimeout(async () => {
      setBuscaCarregando(true)
      setBuscaErro(null)
      try {
        const res = await api.buscarNoArtigo(artigo.id, q)
        setBuscaResultados(res)
        setBuscaIdx(0)
        if (leitorUser?.id) { try { await cacheBusca(leitorUser.id, artigo.id, q, res) } catch {} }
        if (res.length > 0) {
          const first = res[0]
          setBuscaFoco({ pagina: first.pagina, pos: first.pos, nonce: Date.now() })
          setPaginaAtual(first.pagina)
          requestAnimationFrame(() => {
            paginaRefs.current.get(first.pagina)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
          })
        } else {
          setBuscaFoco(null)
        }
      } catch (e) {
        if (isNetworkErrorApp(e) && leitorUser?.id) {
          const cached = await loadBusca(leitorUser.id, artigo.id, q)
          if (cached) {
            setBuscaResultados(cached)
            setBuscaIdx(0)
            if (cached.length > 0) {
              const first = cached[0]
              setBuscaFoco({ pagina: first.pagina, pos: first.pos, nonce: Date.now() })
              setPaginaAtual(first.pagina)
              requestAnimationFrame(() => {
                paginaRefs.current.get(first.pagina)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
              })
            } else setBuscaFoco(null)
            setBuscaErro(null)
          } else {
            setBuscaErro('Sem conexão — nenhuma busca cacheada para este termo')
            setBuscaResultados([])
            setBuscaFoco(null)
          }
        } else {
          const msg = e instanceof Error ? e.message : 'Falha na busca'
          setBuscaErro(msg)
          setBuscaResultados([])
          setBuscaFoco(null)
        }
      } finally {
        setBuscaCarregando(false)
      }
    }, 300)
    return () => window.clearTimeout(t)
  }, [buscaQuery, buscaAberta, artigo.id, leitorUser?.id])

  useEffect(() => {
    if (aba !== 'historico' || historico !== null) return
    let cancelado = false
    const doLoad = async () => {
      try {
        const h = await api.listarHistorico(artigo.id)
        if (!cancelado) {
          setHistorico(h)
          setHistoricoErro(false)
          if (leitorUser?.id) { try { await cacheHistorico(leitorUser.id, artigo.id, h) } catch {} }
        }
      } catch (e) {
        if (!cancelado) {
          if (isNetworkErrorApp(e) && leitorUser?.id) {
            const cached = await loadHistorico(leitorUser.id, artigo.id)
            if (cached) {
              setHistorico(cached)
              setHistoricoErro(false)
              return
            }
          }
          setHistorico(null)
          setHistoricoErro(true)
        }
      }
    }
    void doLoad()
    return () => { cancelado = true }
  }, [aba, historico, historicoNonce, artigo.id, leitorUser?.id])

  // refs para evitar loop infinito por deps citacoes/carregando (milhares de fetch)
  const citacoesRef = useRef(citacoes)
  const carregandoRef = useRef(citacoesCarregando)
  useEffect(() => {
    citacoesRef.current = citacoes
  }, [citacoes])
  useEffect(() => {
    carregandoRef.current = citacoesCarregando
  }, [citacoesCarregando])

  useEffect(() => {
    if (aba !== 'citacoes') return
    if (citacoesRef.current !== null) return
    if (carregandoRef.current) return
    let cancelado = false
    setCitacoesCarregando(true)
    setCitacoesErro(null)
    const timeout = (ms: number, msg: string) =>
      new Promise<never>((_, rej) => window.setTimeout(() => rej(new Error(msg)), ms))
    const comTimeout = <T,>(p: Promise<T>, ms: number, msg: string): Promise<T> =>
      Promise.race([p, timeout(ms, msg)]) as Promise<T>
    comTimeout(api.listarCitacoes(artigo.id), 10000, 'Tempo esgotado ao listar citações')
      .then(async (lista) => {
        if (cancelado) {
          varrerJaTentado.current = false
          setCitacoesCarregando(false)
          return
        }
        if (lista.length === 0 && !varrerJaTentado.current) {
          if (!navigator.onLine) {
            // offline: não varrer silenciosamente
            if (leitorUser?.id) {
              const cached = await loadCitacoes(leitorUser.id, artigo.id)
              if (cached) {
                if (!cancelado) setCitacoes(cached)
              } else {
                if (!cancelado) {
                  setCitacoesErro('Sem conexão — citações precisam de internet; mostrando último cache vazio')
                  setCitacoes([])
                }
              }
            } else {
              if (!cancelado) { setCitacoesErro('Sem conexão — conecte para varrer citações'); setCitacoes([]) }
            }
            setCitacoesCarregando(false)
            return
          }
          varrerJaTentado.current = true
          try {
            const varridas = await comTimeout(api.varrerCitacoes(artigo.id), 12000, 'Tempo esgotado ao varrer citações')
            if (cancelado) {
              varrerJaTentado.current = false
              setCitacoesCarregando(false)
              return
            }
            setCitacoes(varridas)
            if (leitorUser?.id) { try { await cacheCitacoes(leitorUser.id, artigo.id, varridas) } catch {} }
          } catch (e) {
            varrerJaTentado.current = false
            if (!cancelado) {
              if (isNetworkErrorApp(e) && leitorUser?.id) {
                const cached = await loadCitacoes(leitorUser.id, artigo.id)
                if (cached) { setCitacoes(cached); setCitacoesErro(null) }
                else { setCitacoesErro(e instanceof Error ? e.message : 'Falha ao varrer citações'); setCitacoes([]) }
              } else {
                setCitacoesErro(e instanceof Error ? e.message : 'Falha ao varrer citações')
                setCitacoes([])
              }
            }
          } finally {
            setCitacoesCarregando(false)
          }
          return
        }
        if (!cancelado) {
          setCitacoes(lista)
          if (leitorUser?.id) { try { await cacheCitacoes(leitorUser.id, artigo.id, lista) } catch {} }
        }
        setCitacoesCarregando(false)
      })
      .catch(async (e) => {
        varrerJaTentado.current = false
        if (!cancelado) {
          if (isNetworkErrorApp(e) && leitorUser?.id) {
            const cached = await loadCitacoes(leitorUser.id, artigo.id)
            if (cached) { setCitacoes(cached); setCitacoesErro(null) }
            else { setCitacoesErro(e instanceof Error ? e.message : 'Falha ao carregar citações'); setCitacoes([]) }
          } else {
            setCitacoesErro(e instanceof Error ? e.message : 'Falha ao carregar citações')
            setCitacoes([])
          }
        }
        setCitacoesCarregando(false)
      })
    return () => {
      cancelado = true
    }
  }, [aba, artigo.id, leitorUser?.id])

  // reset citacoes ao trocar de artigo
  useEffect(() => {
    setCitacoes(null)
    setCitacoesCarregando(false)
    setCitacoesErro(null)
    varrerJaTentado.current = false
    setFonteFoco(null)
    setBuscaFoco(null)
    setBuscaResultados([])
    setBuscaQuery('')
    setBuscaAberta(false)
    ocorrenciaIdxRef.current.clear()
    setSumario(null)
    setSumarioCarregando(false)
    setSumarioErro(null)
  }, [artigo.id])

  const criarMarcacao = useCallback(
    async (nova: { pagina: number; cor: string; palavras: PalavraBox[]; texto: string }): Promise<Marcacao | null> => {
      if (!leitorUser?.id) { mostrarAviso('Faça login para criar destaque'); return null }
      const tempId = genTempId()
      const clientId = genUUID()
      const temp: Marcacao = { id: tempId, pagina: nova.pagina, tipo: 'highlight', cor: nova.cor, palavras: nova.palavras, texto: nova.texto }
      setMarcacoes(prev => [...prev, temp])
      try { await upsertCachedMarcacao(leitorUser.id, artigo.id, temp, { clientId, version: 0 }) } catch {}
      try {
        await enqueueOperation({ userId: leitorUser.id, artigoId: artigo.id, entity: 'marcacao', action: 'create', data: { artigo_id: artigo.id, artigoId: artigo.id, pagina: nova.pagina, tipo: 'highlight', cor: nova.cor, palavras: nova.palavras, texto: nova.texto }, baseVersion: 0, id: tempId, clientId })
        void syncNow(leitorUser.id)
        invalidarHistorico()
        return temp
      } catch (e) {
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao enfileirar destaque')
        return temp
      }
    },
    [artigo.id, leitorUser?.id, invalidarHistorico, mostrarAviso],
  )

  const removerMarcacao = useCallback(
    async (marcacao: Marcacao) => {
      if (!leitorUser?.id) { mostrarAviso('Faça login'); return }
      const prev = marcacao
      // versão/clientId ANTES de apagar cache (cache é a fonte)
      const ver = await getCachedMarcVersion(leitorUser.id, artigo.id, marcacao.id).catch(() => null)
      setMarcacoes(cur => cur.filter(m => m.id !== marcacao.id))
      try { await deleteCachedMarcacao(leitorUser.id, artigo.id, marcacao.id) } catch {}
      if (marcacao.id < 0) {
        try { await cancelPendingOperationsForTempId(leitorUser.id, artigo.id, 'marcacao', marcacao.id) } catch {}
        invalidarHistorico()
        return
      }
      const baseVersion = ver?.version ?? 0
      const clientId = ver?.clientId ?? genUUID()
      try {
        await enqueueOperation({ userId: leitorUser.id, artigoId: artigo.id, entity: 'marcacao', action: 'delete', data: {}, baseVersion, id: marcacao.id, clientId })
        void syncNow(leitorUser.id)
        invalidarHistorico()
      } catch (e) {
        setMarcacoes(cur => [...cur, prev])
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao remover o destaque')
      }
    },
    [artigo.id, leitorUser?.id, invalidarHistorico, mostrarAviso],
  )

  const atualizarCorMarcacao = useCallback(
    async (marcacao: Marcacao, cor: string) => {
      if (!leitorUser?.id) { mostrarAviso('Faça login'); return }
      const prevCor = marcacao.cor
      setMarcacoes(cur => cur.map(m => m.id === marcacao.id ? { ...m, cor } : m))
      try {
        const patched = { ...marcacao, cor }
        await upsertCachedMarcacao(leitorUser.id, artigo.id, patched, { version: (await getCachedMarcVersion(leitorUser.id, artigo.id, marcacao.id).catch(()=>null))?.version ?? 0 })
      } catch {}
      if (marcacao.id < 0) {
        try { await mergePendingCreateData(leitorUser.id, artigo.id, 'marcacao', marcacao.id, { cor }) } catch {}
        return
      }
      const ver = await getCachedMarcVersion(leitorUser.id, artigo.id, marcacao.id).catch(() => null)
      const baseVersion = ver?.version ?? 0
      const clientId = ver?.clientId ?? genUUID()
      try {
        await enqueueOperation({ userId: leitorUser.id, artigoId: artigo.id, entity: 'marcacao', action: 'update', data: { cor }, baseVersion, id: marcacao.id, clientId })
        void syncNow(leitorUser.id)
        invalidarHistorico()
      } catch (e) {
        setMarcacoes(cur => cur.map(m => m.id === marcacao.id ? { ...m, cor: prevCor } : m))
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao trocar a cor do destaque')
      }
    },
    [artigo.id, leitorUser?.id, invalidarHistorico, mostrarAviso],
  )

  const criarNota = useCallback(
    async (pagina: number, texto: string, marcacaoId?: number, tags?: string[], cor?: string): Promise<boolean> => {
      if (!leitorUser?.id) { mostrarAviso('Faça login'); return false }
      const tempId = genTempId()
      const clientId = genUUID()
      const temp: Nota = { id: tempId, pagina, texto, criado_em: new Date().toISOString(), tags: tags ?? [], cor: cor ?? '#FFEB3B', marcacao_id: marcacaoId }
      setNotas(prev => [...prev, temp])
      try { await upsertCachedNota(leitorUser.id, artigo.id, temp, { clientId, version: 0 }) } catch {}
      try {
        await enqueueOperation({ userId: leitorUser.id, artigoId: artigo.id, entity: 'nota', action: 'create', data: { artigo_id: artigo.id, artigoId: artigo.id, pagina, texto, marcacao_id: marcacaoId, tags, cor }, baseVersion: 0, id: tempId, clientId })
        void syncNow(leitorUser.id)
        setPrefill(null)
        invalidarHistorico()
        return true
      } catch (e) {
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao salvar a nota')
        return false
      }
    },
    [artigo.id, leitorUser?.id, invalidarHistorico, mostrarAviso],
  )

  const removerNota = useCallback(
    async (nota: Nota) => {
      if (!leitorUser?.id) { mostrarAviso('Faça login'); return }
      setRemovendoNotaId(nota.id)
      const prev = nota
      const ver = await getCachedNotaVersion(leitorUser.id, artigo.id, nota.id).catch(()=>null)
      setNotas(cur => cur.filter(n => n.id !== nota.id))
      try { await deleteCachedNota(leitorUser.id, artigo.id, nota.id) } catch {}
      if (nota.id < 0) { try { await cancelPendingOperationsForTempId(leitorUser.id, artigo.id, 'nota', nota.id) } catch {}; setRemovendoNotaId(null); invalidarHistorico(); return }
      // ver já capturado antes do delete
      const baseVersion = ver?.version ?? 0
      const clientId = ver?.clientId ?? genUUID()
      try {
        await enqueueOperation({ userId: leitorUser.id, artigoId: artigo.id, entity: 'nota', action: 'delete', data: {}, baseVersion, id: nota.id, clientId })
        void syncNow(leitorUser.id)
        invalidarHistorico()
      } catch (e) {
        setNotas(cur => [...cur, prev])
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao excluir a nota')
      } finally {
        setRemovendoNotaId(null)
      }
    },
    [artigo.id, leitorUser?.id, invalidarHistorico, mostrarAviso],
  )

  // recarrega do cache após sync para refletir pull/tombstones e remapeamento de IDs
  useEffect(() => {
    if (!leitorUser?.id) return
    const unsub = subscribeSyncStatus(() => {
      if (!leitorUser?.id) return
      void (async () => {
        try {
          const [m, n] = await Promise.all([loadMarcacoes(leitorUser.id, artigo.id), loadNotas(leitorUser.id, artigo.id)])
          // merge: preserva temporários negativos ainda não mapeados
          // pull já evita sobrescrever pendente, mas recarregar cache pode trazer server ids
          if (m.length) setMarcacoes(cur => {
            const temps = cur.filter(x => x.id < 0)
            // se já temos server version do mesmo client, descarta temp
            const serverIds = new Set(m.map(x=>x.id))
            const keepTemps = temps.filter(t => !serverIds.has(Math.abs(t.id))) // heuristic: temp id abs not equal server ids
            // evita duplicar: se m já contém mapeado, não precisa temp
            return temps.length ? [...m, ...keepTemps] : m
          })
          if (n.length) setNotas(cur => {
            const temps = cur.filter(x => x.id < 0)
            return temps.length ? [...n, ...temps.filter(t=> !n.some(nn=> nn.texto===t.texto && nn.pagina===t.pagina))] : n
          })
        } catch {}
      })()
    })
    return unsub
  }, [artigo.id, leitorUser?.id])

  const notaDaSelecao = useCallback((pagina: number, texto: string, marcacaoId?: number) => {
    setAba('notas')
    setPrefill({ texto, pagina, marcacaoId })
    setPrefillNonce((n) => n + 1)
  }, [])

  const abrirNota = useCallback((nota: Nota) => {
    setAba('notas')
    setPainelAberto(true)
    setNotaFoco({ id: nota.id, nonce: Date.now() })
  }, [])

  const numeroDaNota = useCallback(
    (notaId: number) => {
      const idx = [...notas].sort((a, b) => a.id - b.id).findIndex((n) => n.id === notaId)
      return idx >= 0 ? idx + 1 : 0
    },
    [notas],
  )

  const irParaPagina = useCallback((numero: number) => {
    setPaginaAtual(numero)
    paginaRefs.current.get(numero)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, [])

  const limparFonteFoco = useCallback(() => setFonteFoco(null), [])

  const irParaFonte = useCallback(
    (citacao: Citacao) => {
      const pagina0 = (citacao as Citacao & { pagina?: number }).pagina ?? 0
      const pos0 = (citacao as Citacao & { pos?: [number, number, number, number] | null }).pos ?? null
      const ocorrencias = (citacao as Citacao & { ocorrencias?: Array<{ pagina: number; pos: [number, number, number, number] | null; trecho: string }> }).ocorrencias ?? []
      const temAlgumaValida = ocorrencias.some((o) => o.pagina > 0 && o.pos)
      const lista: Array<{ pagina: number; pos: [number, number, number, number] | null }> =
        ocorrencias.length > 0
          ? ocorrencias
          : pagina0 > 0 && pos0
            ? [{ pagina: pagina0, pos: pos0 }]
            : []

      if (lista.length === 0 || (!temAlgumaValida && lista.every((o) => !o.pos || o.pagina === 0))) {
        if (!pagina0 || !pos0) {
          mostrarAviso('Sem ocorrência no corpo do texto')
          return
        }
      }

      const navegaveis = lista.filter((o) => o.pagina > 0 && o.pos)
      if (navegaveis.length === 0) {
        mostrarAviso('Sem ocorrência no corpo do texto')
        return
      }

      const prev = ocorrenciaIdxRef.current.get(citacao.id) ?? -1
      const nextIdx = (prev + 1) % navegaveis.length
      ocorrenciaIdxRef.current.set(citacao.id, nextIdx)
      const alvo = navegaveis[nextIdx]
      setFonteFoco({ citacaoId: citacao.id, pagina: alvo.pagina, pos: alvo.pos!, nonce: Date.now() })
      setBuscaFoco(null)
      setPaginaAtual(alvo.pagina)
      requestAnimationFrame(() => {
        paginaRefs.current.get(alvo.pagina)?.scrollIntoView({ behavior: 'smooth', block: 'start' })
      })
    },
    [mostrarAviso],
  )

  const enriquecerCitacao = useCallback(
    async (citacao: Citacao) => {
      if (!navigator.onLine) { mostrarAviso('Sem conexão — enriquecer precisa de internet'); return }
      setEnriquecendoId(citacao.id)
      try {
        const res = await api.enriquecerCitacao(artigo.id, citacao.id)
        if (res.url) {
          setCitacoes((prev) => (prev ? prev.map((c) => (c.id === citacao.id ? { ...c, url: res.url } : c)) : prev))
          if (leitorUser?.id && citacoes) {
            const updated = (citacoes ?? []).map(c => c.id === citacao.id ? { ...c, url: res.url } : c)
            try { await cacheCitacoes(leitorUser.id, artigo.id, updated) } catch {}
          }
        } else {
          mostrarAviso('Nenhuma fonte encontrada para esta citação')
        }
      } catch (e) {
        if (isNetworkErrorApp(e)) mostrarAviso('Sem conexão — não foi possível enriquecer')
        else mostrarAviso(e instanceof Error ? e.message : 'Falha ao buscar fonte')
      } finally {
        setEnriquecendoId(null)
      }
    },
    [artigo.id, mostrarAviso, leitorUser?.id, citacoes],
  )

  const abrirExterno = useCallback(
    async (url: string) => {
      try {
        const parsed = new URL(url)
        if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') throw new Error('URL inválida')
      } catch {
        mostrarAviso('URL inválida')
        return
      }
      try {
        if (window.artigosAna?.abrirExterno) {
          await window.artigosAna.abrirExterno(url)
        } else {
          window.open(url, '_blank', 'noopener')
        }
      } catch (e) {
        mostrarAviso(e instanceof Error ? e.message : 'Falha ao abrir link')
      }
    },
    [mostrarAviso],
  )

  const recarregarCitacoes = useCallback(() => {
    if (!navigator.onLine) { mostrarAviso('Sem conexão — varrer citações precisa de internet'); return }
    setCitacoes(null)
    setCitacoesErro(null)
    setCitacoesCarregando(false)
    varrerJaTentado.current = false
    setAba('citacoes')
  }, [mostrarAviso])

  const mudarPagina = (delta: number) => {
    if (!detalhe) return
    const alvo = Math.min(Math.max(paginaAtual + delta, 1), detalhe.paginas.length)
    irParaPagina(alvo)
  }

  const exportar = useCallback(async () => {
    if (!detalhe) return
    if (!navigator.onLine) {
      setExportEstado({ fase: 'erro', resultado: null, erro: 'Sem conexão — exportar DOCX precisa de internet. Reconecte e tente novamente.' })
      return
    }
    setExportEstado({ fase: 'exportando', resultado: null, erro: null })
    try {
      const resultado = await api.exportar(detalhe.id)
      setExportEstado({ fase: 'pronto', resultado, erro: null })
    } catch (e) {
      if (isNetworkErrorApp(e)) {
        setExportEstado({ fase: 'erro', resultado: null, erro: 'Sem conexão — exportar exige internet' })
      } else {
        setExportEstado({
          fase: 'erro',
          resultado: null,
          erro: e instanceof Error ? e.message : 'Falha ao exportar',
        })
      }
    }
  }, [detalhe])

  if (!detalhe) {
    return (
      <div className="leitor">
        <div className="leitor-erro">
          <div className="leitor-erro-card">
            <p>{erro ?? 'Carregando artigo…'}</p>
            {erro && (
              <button type="button" className="btn" onClick={onVoltar}>
                Voltar para a biblioteca
              </button>
            )}
          </div>
        </div>
      </div>
    )
  }

  return (
    <div className={`leitor ${painelAberto ? 'leitor--painel-aberto' : 'leitor--painel-fechado'}`}>
      <div className="leitor-toolbar">
        <button
          type="button"
          className="btn btn-icon"
          onClick={onVoltar}
          title="Voltar para a biblioteca"
          aria-label="Voltar para a biblioteca"
        >
          <IconeVoltar />
        </button>
        <span className="leitor-titulo" title={detalhe.titulo}>
          {detalhe.titulo}
        </span>
        <div className="leitor-spacer" />
        <button
          type="button"
          className="btn btn-icon"
          onClick={abrirBusca}
          title="Buscar no texto (Ctrl+K)"
          aria-label="Buscar no texto"
        >
          <IconeBusca size={15} />
        </button>
        <button
          type="button"
          className="btn btn-icon btn-painel"
          onClick={() => setPainelAberto((v) => !v)}
          title={painelAberto ? 'Fechar painel' : 'Abrir painel'}
          aria-label={painelAberto ? 'Fechar painel' : 'Abrir painel'}
        >
          <IconeNota size={15} />
        </button>
        <div className="leitor-paginacao">
          <button
            type="button"
            className="btn btn-icon"
            onClick={() => mudarPagina(-1)}
            disabled={paginaAtual <= 1}
            title="Página anterior"
            aria-label="Página anterior"
          >
            <IconeSetaEsquerda size={15} />
          </button>
          <span>
            Página {paginaAtual} de {detalhe.paginas.length}
          </span>
          <button
            type="button"
            className="btn btn-icon"
            onClick={() => mudarPagina(1)}
            disabled={paginaAtual >= detalhe.paginas.length}
            title="Próxima página"
            aria-label="Próxima página"
          >
            <IconeSetaDireita size={15} />
          </button>
        </div>
        <button
          type="button"
          className="btn btn-primary"
          onClick={() => void exportar()}
          disabled={exportEstado?.fase === 'exportando'}
        >
          <IconeDownload size={15} />
          Exportar
        </button>
      </div>

      {buscaAberta && (
        <div className="leitor-busca-barra" role="search">
          <span className="leitor-busca-icone">
            <IconeBusca size={15} />
          </span>
          <input
            ref={buscaInputRef}
            className="input leitor-busca-input"
            type="search"
            placeholder="Buscar no texto…"
            value={buscaQuery}
            onChange={(e) => setBuscaQuery(e.target.value)}
            aria-label="Buscar no texto do PDF"
            onKeyDown={(e) => {
              if (e.key === 'Enter') {
                if (e.shiftKey) irParaBusca(buscaIdx - 1)
                else irParaBusca(buscaIdx + 1)
              }
              if (e.key === 'Escape') fecharBusca()
            }}
          />
          <span className="leitor-busca-contador">
            {buscaCarregando ? 'buscando…' : buscaErro ? buscaErro : buscaQuery.trim().length >= 2 ? (buscaResultados.length === 0 ? '0 ocorrências' : `${buscaResultados.length} ocorrências — ${buscaResultados.length > 0 ? `${buscaIdx + 1}/${buscaResultados.length}` : ''}`) : ''}
          </span>
          <button
            type="button"
            className="btn btn-icon"
            disabled={buscaResultados.length === 0}
            onClick={() => irParaBusca(buscaIdx - 1)}
            title="Anterior"
            aria-label="Ocorrência anterior"
          >
            <IconeSetaEsquerda size={15} />
          </button>
          <button
            type="button"
            className="btn btn-icon"
            disabled={buscaResultados.length === 0}
            onClick={() => irParaBusca(buscaIdx + 1)}
            title="Próxima"
            aria-label="Próxima ocorrência"
          >
            <IconeSetaDireita size={15} />
          </button>
          <button
            type="button"
            className="btn btn-icon"
            onClick={fecharBusca}
            title="Fechar busca"
            aria-label="Fechar busca"
          >
            <IconeFechar size={15} />
          </button>
        </div>
      )}

      <div className="leitor-corpo">
        <div className="leitor-canvas">
          <div className="leitor-paginas">
            <div className="paginas-coluna">
              {detalhe.paginas.map((p) => (
                <div
                  key={p.numero}
                  ref={(el) => {
                    if (el) paginaRefs.current.set(p.numero, el)
                    else paginaRefs.current.delete(p.numero)
                  }}
                >
                  <PageView
                    artigoId={detalhe.id}
                    pagina={p}
                    marcacoes={marcacoes.filter((m) => m.pagina === p.numero)}
                    notas={notas}
                    numeroDaNota={numeroDaNota}
                    onAbrirNota={abrirNota}
                    onCriarMarcacao={criarMarcacao}
                    onCriarNotaSelecao={notaDaSelecao}
                    onRemoverMarcacao={removerMarcacao}
                    onAtualizarCorMarcacao={atualizarCorMarcacao}
                    fonteFoco={fonteFoco}
                    onLimparFonteFoco={limparFonteFoco}
                    buscaFoco={buscaFoco}
                    onLimparBuscaFoco={limparBuscaFoco}
                  />
                </div>
              ))}
            </div>
          </div>
          <IlhaNota
            numPaginas={detalhe.paginas.length}
            prefill={prefill}
            prefillNonce={prefillNonce}
            onCriarNota={criarNota}
          />
        </div>

        <Suspense fallback={<div className="estado-vazio" style={{ width: 320 }}><p style={{ fontSize: 'var(--fs-sm)' }}>Carregando painel…</p></div>}>
          <PainelLateral
            notas={notas}
            historico={historico}
            historicoErro={historicoErro}
            aba={aba}
            onMudarAba={setAba}
            onRemoverNota={removerNota}
            onIrParaPagina={irParaPagina}
            removendoId={removendoNotaId}
            notaFoco={notaFoco}
            numeroDaNota={numeroDaNota}
            aberto={painelAberto}
            citacoes={citacoes}
            citacoesCarregando={citacoesCarregando}
            citacoesErro={citacoesErro}
            enriquecendoId={enriquecendoId}
            onEnriquecer={enriquecerCitacao}
            onAbrirExterno={abrirExterno}
            onRecarregarCitacoes={recarregarCitacoes}
            onIrParaFonte={irParaFonte}
            sumario={sumario}
            sumarioCarregando={sumarioCarregando}
            sumarioErro={sumarioErro}
            onFechar={() => setPainelAberto(false)}
          />
        </Suspense>
      </div>

      {aviso && (
        <div className="toast">
          <div className="aviso">
            <span>{aviso}</span>
          </div>
        </div>
      )}

      {exportEstado && (
        <div className="dialog-overlay">
          <div className="dialog" role="dialog" aria-modal="true">
            <h2 className="dialog-titulo">Exportar para DOCX</h2>
            {exportEstado.fase === 'exportando' && (
              <>
                <p className="dialog-mensagem">Gerando o documento… isso pode levar alguns segundos.</p>
                <div className="progresso">
                  <div className="progresso-barra" />
                </div>
              </>
            )}
            {exportEstado.fase === 'erro' && (
              <>
                <p className="dialog-mensagem">{exportEstado.erro}</p>
                <div className="dialog-acoes">
                  <button type="button" className="btn" onClick={() => setExportEstado(null)}>
                    Fechar
                  </button>
                  <button type="button" className="btn btn-primary" onClick={() => void exportar()}>
                    Tentar novamente
                  </button>
                </div>
              </>
            )}
            {exportEstado.fase === 'pronto' && exportEstado.resultado && (
              <>
                <p className="dialog-mensagem">Documento gerado com sucesso.</p>
                <div className="dialog-caminho">{exportEstado.resultado.caminho}</div>
                <div className="dialog-acoes">
                  <button type="button" className="btn" onClick={() => setExportEstado(null)}>
                    Fechar
                  </button>
                  <button
                    type="button"
                    className="btn"
                    onClick={() => window.artigosAna?.abrirPasta(exportEstado.resultado!.caminho)}
                  >
                    Abrir pasta
                  </button>
                  <button
                    type="button"
                    className="btn btn-primary"
                    onClick={() => window.artigosAna?.abrirArquivo(exportEstado.resultado!.caminho)}
                  >
                    Abrir arquivo
                  </button>
                </div>
              </>
            )}
          </div>
        </div>
      )}
    </div>
  )
}
