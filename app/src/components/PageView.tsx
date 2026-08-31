import { useCallback, useEffect, useRef, useState } from 'react'
import {
  api,
  type CamadaTexto,
  type Marcacao,
  type Nota,
  type PaginaInfo,
  type PalavraBox,
} from '../api'
import { cacheCamada, loadCamada, cacheImagem, loadImagem } from '../offline/cache'
import { loadLatestValidSession } from '../offline/session'

function isNetErr(e: unknown): boolean {
  if (e instanceof TypeError) return true
  const m = e instanceof Error ? e.message : String(e)
  return /Failed to fetch|NetworkError|network|Load failed/i.test(m)
}
async function currentUserId(): Promise<number | null> {
  try { const s = await loadLatestValidSession(); return s?.id ?? null } catch { return null }
}
import {
  agruparEmSegmentos,
  agruparTexto,
  boxesDasPalavras,
  indicePalavraProxima,
  palavrasNoSegmento,
  posicaoDePalavra,
} from './geometry'

export const CORES_DESTAQUE = [
  { nome: 'Amarelo', valor: '#FFE03B' },
  { nome: 'Verde', valor: '#9EE6A8' },
  { nome: 'Azul', valor: '#8FC1FF' },
  { nome: 'Rosa', valor: '#FFB3D1' },
]

interface SelAtiva {
  x: number
  y: number
  pagina: number
  palavras: PalavraBox[]
  texto: string
}

interface PopoverAtivo {
  x: number
  y: number
  marcacao: Marcacao
}

export interface FonteFoco {
  citacaoId: number
  pagina: number
  pos: [number, number, number, number]
  nonce: number
}

export interface BuscaFoco {
  pagina: number
  pos: [number, number, number, number]
  nonce: number
}

interface Props {
  artigoId: number
  pagina: PaginaInfo
  marcacoes: Marcacao[]
  notas: Nota[]
  numeroDaNota: (notaId: number) => number
  onAbrirNota: (nota: Nota) => void
  onCriarMarcacao: (nova: {
    pagina: number
    cor: string
    palavras: PalavraBox[]
    texto: string
  }) => Promise<Marcacao | null>
  onCriarNotaSelecao: (pagina: number, texto: string, marcacaoId?: number) => void
  onRemoverMarcacao: (marcacao: Marcacao) => Promise<void>
  onAtualizarCorMarcacao: (marcacao: Marcacao, cor: string) => Promise<void>
  fonteFoco?: FonteFoco | null
  onLimparFonteFoco?: () => void
  buscaFoco?: BuscaFoco | null
  onLimparBuscaFoco?: () => void
}

const TOOLTIP_LARGURA = 240
const TOOLTIP_ALTURA = 46
const POPOVER_LARGURA = 170

export function PageView({
  artigoId,
  pagina,
  marcacoes,
  notas,
  numeroDaNota,
  onAbrirNota,
  onCriarMarcacao,
  onCriarNotaSelecao,
  onRemoverMarcacao,
  onAtualizarCorMarcacao,
  fonteFoco,
  onLimparFonteFoco,
  buscaFoco,
  onLimparBuscaFoco,
}: Props) {
  const containerRef = useRef<HTMLDivElement>(null)
  const overlayRef = useRef<HTMLDivElement>(null)
  const tooltipRef = useRef<HTMLDivElement>(null)
  const popoverRef = useRef<HTMLDivElement>(null)

  const [ativo, setAtivo] = useState(false)
  const [camada, setCamada] = useState<CamadaTexto | null>(null)
  const [larguraPx, setLarguraPx] = useState(0)
  const [selecao, setSelecao] = useState<SelAtiva | null>(null)
  const [popover, setPopover] = useState<PopoverAtivo | null>(null)
  const [apagarArmado, setApagarArmado] = useState(false)
  const apagarTimer = useRef<number | null>(null)
  const [preview, setPreview] = useState<PalavraBox[] | null>(null)
  const [imagemSrc, setImagemSrc] = useState<string | null>(null)
  const imagemObjectUrlRef = useRef<string | null>(null)
  const arrastoRef = useRef<{
    inicio: { x: number; y: number; clientX: number; clientY: number; time: number }
    ultimo: { x: number; y: number }
    indices: Set<number>
    decidido: boolean
  } | null>(null)
  const [fontePulso, setFontePulso] = useState(true)
  const [buscaPulso, setBuscaPulso] = useState(true)

  // rAF throttling for preview (iPad 60fps: avoid setState thrash on pointermove)
  const previewRafRef = useRef<number | null>(null)
  const pendingPreviewRef = useRef<PalavraBox[] | null>(null)
  const schedulePreview = useCallback((boxes: PalavraBox[] | null) => {
    pendingPreviewRef.current = boxes
    if (previewRafRef.current != null) return
    previewRafRef.current = requestAnimationFrame(() => {
      previewRafRef.current = null
      const next = pendingPreviewRef.current
      pendingPreviewRef.current = null
      setPreview(next)
    })
  }, [])

  useEffect(() => {
    return () => {
      if (previewRafRef.current != null) cancelAnimationFrame(previewRafRef.current)
    }
  }, [])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    if (typeof IntersectionObserver === 'undefined') {
      setAtivo(true)
      return
    }
    // iPad virtualization: viewport + 2 pages buffer (~800px). Toggle ativo to free image decoder
    // Uses .leitor-paginas as root when available (precise scroll container)
    const scrollRoot = (el.closest('.leitor-paginas') as Element | null) ?? null
    const io = new IntersectionObserver(
      (entries) => {
        const entry = entries[0]
        if (!entry) return
        setAtivo(entry.isIntersecting)
      },
      { root: scrollRoot, rootMargin: '800px 0px', threshold: 0 },
    )
    io.observe(el)
    return () => io.disconnect()
  }, [])

  useEffect(() => {
    const el = containerRef.current
    if (!el) return
    if (typeof ResizeObserver === 'undefined') return
    let rafId: number | null = null
    const ro = new ResizeObserver((entries) => {
      const w = entries[0]?.contentRect.width ?? 0
      if (rafId != null) cancelAnimationFrame(rafId)
      rafId = requestAnimationFrame(() => {
        rafId = null
        setLarguraPx(w)
      })
    })
    ro.observe(el)
    return () => {
      if (rafId != null) cancelAnimationFrame(rafId)
      ro.disconnect()
    }
  }, [])

  useEffect(() => {
    if (!ativo || camada) return
    let cancelado = false
    const load = async () => {
      try {
        const c = await api.getCamada(artigoId, pagina.numero)
        if (!cancelado) {
          setCamada(c)
          const uid = await currentUserId()
          if (uid != null) { try { await cacheCamada(uid, artigoId, pagina.numero, c) } catch {} }
        }
      } catch (e) {
        if (!cancelado) {
          if (isNetErr(e)) {
            const uid = await currentUserId()
            if (uid != null) {
              const cached = await loadCamada(uid, artigoId, pagina.numero)
              if (cached) { setCamada(cached); return }
            }
          }
          setCamada(null)
        }
      }
    }
    void load()
    return () => { cancelado = true }
  }, [ativo, camada, artigoId, pagina.numero])

  useEffect(() => {
    let cancelado = false
    let novoUrl: string | null = null
    const ac = new AbortController()

    if (!ativo) {
      if (imagemObjectUrlRef.current) {
        URL.revokeObjectURL(imagemObjectUrlRef.current)
        imagemObjectUrlRef.current = null
      }
      setImagemSrc(null)
      return () => {
        cancelado = true
        ac.abort()
      }
    }

    if (imagemObjectUrlRef.current) {
      URL.revokeObjectURL(imagemObjectUrlRef.current)
      imagemObjectUrlRef.current = null
    }
    setImagemSrc(null)

    const doFetch = async () => {
      try {
        const blob = await api.getImagem(artigoId, pagina.numero, ac.signal)
        if (cancelado || ac.signal.aborted) return
        const uid = await currentUserId()
        if (uid != null) { try { await cacheImagem(uid, artigoId, pagina.numero, blob) } catch {} }
        novoUrl = URL.createObjectURL(blob)
        imagemObjectUrlRef.current = novoUrl
        setImagemSrc(novoUrl)
      } catch (e) {
        if (e instanceof DOMException && e.name === 'AbortError') return
        if (cancelado) return
        if (isNetErr(e)) {
          const uid = await currentUserId()
          if (uid != null) {
            const cached = await loadImagem(uid, artigoId, pagina.numero)
            if (cached) {
              novoUrl = URL.createObjectURL(cached)
              imagemObjectUrlRef.current = novoUrl
              setImagemSrc(novoUrl)
              return
            }
          }
        }
        setImagemSrc(null)
      }
    }
    void doFetch()

    return () => {
      cancelado = true
      ac.abort()
      if (novoUrl) {
        URL.revokeObjectURL(novoUrl)
        if (imagemObjectUrlRef.current === novoUrl) imagemObjectUrlRef.current = null
      }
    }
  }, [ativo, artigoId, pagina.numero])

  useEffect(() => {
    return () => {
      if (imagemObjectUrlRef.current) {
        URL.revokeObjectURL(imagemObjectUrlRef.current)
        imagemObjectUrlRef.current = null
      }
    }
  }, [])

  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (tooltipRef.current && !tooltipRef.current.contains(e.target as Node)) {
        setSelecao(null)
        setPreview(null)
      }
      if (popoverRef.current && !popoverRef.current.contains(e.target as Node)) {
        setPopover(null)
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  useEffect(() => {
    if (!fonteFoco) return
    if (fonteFoco.pagina !== pagina.numero) return
    setFontePulso(true)
    const t = window.setTimeout(() => setFontePulso(false), 2000)
    return () => window.clearTimeout(t)
  }, [fonteFoco, pagina.numero])

  useEffect(() => {
    if (!buscaFoco) return
    if (buscaFoco.pagina !== pagina.numero) return
    setBuscaPulso(true)
    const t = window.setTimeout(() => setBuscaPulso(false), 2000)
    return () => window.clearTimeout(t)
  }, [buscaFoco, pagina.numero])

  const scale = camada ? larguraPx / camada.largura : 0

  const pontoDoEvento = (e: { clientX: number; clientY: number }) => {
    const overlay = overlayRef.current
    if (!overlay || scale <= 0) return null
    const r = overlay.getBoundingClientRect()
    return { x: (e.clientX - r.left) / scale, y: (e.clientY - r.top) / scale }
  }

  const palavrasDoArrasto = useCallback(() => {
    const arr = arrastoRef.current
    if (!arr || !camada) return []
    const indices = [...arr.indices].sort((a, b) => a - b)
    return indices.map((i) => camada.palavras[i])
  }, [camada])

  const handleOverlayPointerDown = (e: React.PointerEvent) => {
    if (fonteFoco && onLimparFonteFoco) onLimparFonteFoco()
    if (buscaFoco && onLimparBuscaFoco) onLimparBuscaFoco()
    if (!camada) return
    if ((e.target as HTMLElement).closest('.destaque-ret, .nota-badge')) return
    const p = pontoDoEvento(e)
    if (!p) return
    const idx = indicePalavraProxima(camada.palavras, p.x, p.y)
    if (idx == null) return
    arrastoRef.current = {
      inicio: { x: p.x, y: p.y, clientX: e.clientX, clientY: e.clientY, time: Date.now() },
      ultimo: p,
      indices: new Set([idx]),
      decidido: false,
    }
    schedulePreview(boxesDasPalavras([camada.palavras[idx]]))
  }

  const handleOverlayPointerMove = (e: React.PointerEvent) => {
    const arr = arrastoRef.current
    if (!arr || !camada) return
    const p = pontoDoEvento(e)
    if (!p) return

    // Disambiguation: vertical drag => scroll, horizontal/diagonal => marking
    // Uses client pixels for stable 12px threshold (independent of PDF scale)
    // Ratio 1.8 distinguishes pure vertical scroll from diagonal selection
    if (!arr.decidido) {
      const dx = e.clientX - arr.inicio.clientX
      const dy = e.clientY - arr.inicio.clientY
      const absDx = Math.abs(dx)
      const absDy = Math.abs(dy)
      if (absDy > absDx * 1.8 && absDy > 12) {
        // Scroll intent: abort marking, let browser handle pan-y
        arrastoRef.current = null
        schedulePreview(null)
        return
      }
      // Lock as marking only when intent is clear; keep undecided for tiny jitter
      // so a subsequent vertical movement can still abort.
      if (absDx > 8 || Math.max(absDx, absDy) > 12) {
        arr.decidido = true
      }
    }

    for (const i of palavrasNoSegmento(camada.palavras, arr.ultimo.x, arr.ultimo.y, p.x, p.y)) {
      arr.indices.add(i)
    }
    arr.ultimo = p
    const selecionadas = palavrasDoArrasto()
    schedulePreview(boxesDasPalavras(selecionadas))
  }

  const finalizarArrasto = useCallback(() => {
    const selecionadas = palavrasDoArrasto()
    arrastoRef.current = null
    if (!camada || selecionadas.length === 0) return
    const ordenadas = [...selecionadas].sort((a, b) => (a.y0 === b.y0 ? a.x0 - b.x0 : a.y0 - b.y0))
    const palavras = boxesDasPalavras(ordenadas)
    const texto = agruparTexto(ordenadas)
    const ret = {
      x0: Math.min(...ordenadas.map((p) => p.x0)),
      y0: Math.min(...ordenadas.map((p) => p.y0)),
      x1: Math.max(...ordenadas.map((p) => p.x1)),
      y1: Math.max(...ordenadas.map((p) => p.y1)),
    }
    const overlay = overlayRef.current
    if (!overlay) return
    const larguraOverlay = overlay.getBoundingClientRect().width
    let y = ret.y0 * scale - TOOLTIP_ALTURA - 8
    if (y < 4) y = ret.y1 * scale + 8
    const centroX = ((ret.x0 + ret.x1) / 2) * scale
    const x = Math.min(Math.max(centroX - TOOLTIP_LARGURA / 2, 4), Math.max(larguraOverlay - TOOLTIP_LARGURA - 4, 4))
    setPopover(null)
    setSelecao({ x, y, pagina: pagina.numero, palavras, texto })
  }, [palavrasDoArrasto, camada, scale, pagina.numero])

  useEffect(() => {
    const handler = () => {
      if (arrastoRef.current) finalizarArrasto()
    }
    window.addEventListener('pointerup', handler)
    window.addEventListener('pointercancel', handler)
    return () => {
      window.removeEventListener('pointerup', handler)
      window.removeEventListener('pointercancel', handler)
    }
  }, [finalizarArrasto])

  const abrirPopoverParaMarcacao = (marcacao: Marcacao) => {
    const overlay = overlayRef.current
    if (!overlay || scale <= 0) return
    const segs = agruparEmSegmentos(marcacao.palavras)
    if (segs.length === 0) return
    const primeiro = segs[0]
    const o = overlay.getBoundingClientRect()
    let y = primeiro[1] * scale - 130
    if (y < 4) y = Math.max(...segs.map((s) => s[3])) * scale + 6
    const x = Math.min(Math.max(primeiro[0] * scale, 4), o.width - POPOVER_LARGURA - 4)
    setApagarArmado(false)
    setPopover({ x, y, marcacao })
  }

  const destacar = async (cor: string) => {
    if (!selecao) return
    const ativa = selecao
    setSelecao(null)
    setPreview(null)
    const criada = await onCriarMarcacao({ pagina: ativa.pagina, cor, palavras: ativa.palavras, texto: ativa.texto })
    if (criada) abrirPopoverParaMarcacao(criada)
  }

  const notaDaSelecao = async () => {
    if (!selecao) return
    const ativa = selecao
    setSelecao(null)
    setPreview(null)
    const criada = await onCriarMarcacao({
      pagina: ativa.pagina,
      cor: CORES_DESTAQUE[0].valor,
      palavras: ativa.palavras,
      texto: ativa.texto,
    })
    if (criada) onCriarNotaSelecao(ativa.pagina, ativa.texto, criada.id)
  }

  const abrirPopover = (marcacao: Marcacao, e: React.MouseEvent<HTMLDivElement>) => {
    e.stopPropagation()
    const overlay = overlayRef.current
    if (!overlay) return
    const r = e.currentTarget.getBoundingClientRect()
    const o = overlay.getBoundingClientRect()
    const x = Math.max(4, Math.min(r.left - o.left, o.width - POPOVER_LARGURA - 4))
    const y = r.bottom - o.top + 6
    setSelecao(null)
    setPreview(null)
    setApagarArmado(false)
    setPopover({ x, y, marcacao })
  }

  const removerDestaque = async () => {
    if (!popover) return
    if (!apagarArmado) {
      setApagarArmado(true)
      if (apagarTimer.current) window.clearTimeout(apagarTimer.current)
      apagarTimer.current = window.setTimeout(() => setApagarArmado(false), 3000)
      return
    }
    if (apagarTimer.current) window.clearTimeout(apagarTimer.current)
    const alvo = popover.marcacao
    setPopover(null)
    setApagarArmado(false)
    await onRemoverMarcacao(alvo)
  }

  const trocarCorDestaque = async (marcacao: Marcacao, cor: string) => {
    await onAtualizarCorMarcacao(marcacao, cor)
    setPopover((p) => (p ? { ...p, marcacao: { ...p.marcacao, cor } } : p))
  }

  const notaDoDestaque = (marcacao: Marcacao) => {
    setPopover(null)
    onCriarNotaSelecao(marcacao.pagina, marcacao.texto, marcacao.id)
  }

  return (
    <div
      className="pagina"
      ref={containerRef}
      // iPad hint: content-visibility granted via CSS; inline fallback for Safari versions without folhas
      style={{ contentVisibility: 'auto', containIntrinsicSize: 'auto 1550px' } as React.CSSProperties}
    >
      <div
        className="pagina-inner"
        style={{ aspectRatio: `${pagina.largura} / ${pagina.altura}` }}
        onClick={() => {
          if (fonteFoco && onLimparFonteFoco) onLimparFonteFoco()
          if (buscaFoco && onLimparBuscaFoco) onLimparBuscaFoco()
        }}
      >
        {ativo && imagemSrc ? (
          <img
            className="pagina-img"
            src={imagemSrc}
            alt={`Página ${pagina.numero}`}
            draggable={false}
            loading="lazy"
            decoding="async"
          />
        ) : (
          <div
            className="pagina-placeholder"
            style={{ aspectRatio: `${pagina.largura} / ${pagina.altura}` } as React.CSSProperties}
            aria-hidden="true"
          />
        )}
        {larguraPx > 0 && fonteFoco && fonteFoco.pagina === pagina.numero && fonteFoco.pos && (
          <div
            className={`fonte-destaque${fontePulso ? ' pulso' : ''}`}
            data-testid="fonte-destaque"
            aria-hidden="true"
            style={{
              left: fonteFoco.pos[0] * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              top: fonteFoco.pos[1] * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              width: (fonteFoco.pos[2] - fonteFoco.pos[0]) * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              height: (fonteFoco.pos[3] - fonteFoco.pos[1]) * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
            }}
          />
        )}
        {larguraPx > 0 && buscaFoco && buscaFoco.pagina === pagina.numero && buscaFoco.pos && (
          <div
            className={`busca-destaque${buscaPulso ? ' pulso' : ''}`}
            data-testid="busca-destaque"
            aria-hidden="true"
            style={{
              left: buscaFoco.pos[0] * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              top: buscaFoco.pos[1] * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              width: (buscaFoco.pos[2] - buscaFoco.pos[0]) * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
              height: (buscaFoco.pos[3] - buscaFoco.pos[1]) * (camada ? larguraPx / camada.largura : larguraPx / pagina.largura),
            }}
          />
        )}
        {camada && (
          <div
            className="pagina-camada"
            ref={overlayRef}
            onPointerDown={handleOverlayPointerDown}
            onPointerMove={handleOverlayPointerMove}
            onClick={() => {
              if (fonteFoco && onLimparFonteFoco) onLimparFonteFoco()
              if (buscaFoco && onLimparBuscaFoco) onLimparBuscaFoco()
            }}
          >
            {preview &&
              agruparEmSegmentos(preview).map((box, i) => (
                <div
                  key={i}
                  className="sel-preview"
                  style={{
                    left: box[0] * scale,
                    top: box[1] * scale,
                    width: (box[2] - box[0]) * scale,
                    height: (box[3] - box[1]) * scale,
                  }}
                />
              ))}
            {camada.palavras.map((p, i) => (
              <span
                key={i}
                className="palavra"
                style={posicaoDePalavra(p, scale)}
              >
                {p.texto}
              </span>
            ))}

            <div className="pagina-destaques">
              {marcacoes.map((m) =>
                agruparEmSegmentos(m.palavras).map((box, i) => (
                  <div
                    key={`${m.id}-${i}`}
                    className="destaque-ret"
                    style={{
                      left: box[0] * scale,
                      top: box[1] * scale,
                      width: (box[2] - box[0]) * scale,
                      height: (box[3] - box[1]) * scale,
                      background: m.cor,
                      pointerEvents: 'auto',
                    }}
                    title="Destaque"
                    onClick={(e) => abrirPopover(m, e)}
                  />
                )),
              )}
              {marcacoes.map((m) => {
                const nota = notas.find((n) => n.marcacao_id === m.id)
                if (!nota) return null
                const segs = agruparEmSegmentos(m.palavras)
                const ultimo = segs[segs.length - 1]
                if (!ultimo) return null
                return (
                  <button
                    key={`badge-${m.id}`}
                    type="button"
                    className="nota-badge"
                    style={{
                      left: ultimo[2] * scale - 7,
                      top: ultimo[1] * scale - 12,
                    }}
                    title={`Abrir nota ${numeroDaNota(nota.id)}`}
                    aria-label={`Abrir nota ${numeroDaNota(nota.id)}`}
                    onMouseDown={(e) => e.stopPropagation()}
                    onPointerDown={(e) => e.stopPropagation()}
                    onClick={(e) => {
                      e.stopPropagation()
                      onAbrirNota(nota)
                    }}
                  >
                    {numeroDaNota(nota.id)}
                  </button>
                )
              })}
            </div>
          </div>
        )}

        {selecao && (
          <div
            className="tooltip-selecao"
            ref={tooltipRef}
            style={{ left: selecao.x, top: selecao.y }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
          >
            {CORES_DESTAQUE.map((cor) => (
              <button
                key={cor.valor}
                type="button"
                className="tooltip-cor"
                style={{ background: cor.valor }}
                title={`Destacar em ${cor.nome.toLowerCase()}`}
                aria-label={`Destacar em ${cor.nome.toLowerCase()}`}
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => void destacar(cor.valor)}
              />
            ))}
            <div className="tooltip-divisor" />
            <button
              type="button"
              className="btn btn-sm tooltip-acao"
              onMouseDown={(e) => e.preventDefault()}
              onClick={notaDaSelecao}
            >
              Nota
            </button>
          </div>
        )}

        {popover && (
          <div
            className="popover-destaque"
            ref={popoverRef}
            style={{ left: popover.x, top: popover.y }}
            onMouseDown={(e) => e.stopPropagation()}
            onPointerDown={(e) => e.stopPropagation()}
          >
            <span className="popover-titulo">Caneta de destaque</span>
            <div className="popover-cores">
              {CORES_DESTAQUE.map((cor) => (
                <button
                  key={cor.valor}
                  type="button"
                  className={`popover-cor${popover.marcacao.cor === cor.valor ? ' ativa' : ''}`}
                  style={{ background: cor.valor }}
                  title={`Pintar de ${cor.nome.toLowerCase()}`}
                  aria-label={`Pintar de ${cor.nome.toLowerCase()}`}
                  onClick={() => void trocarCorDestaque(popover.marcacao, cor.valor)}
                />
              ))}
            </div>
            <div className="popover-acoes">
              <button
                type="button"
                className="btn btn-sm btn-primary"
                onClick={() => notaDoDestaque(popover.marcacao)}
              >
                Anotar
              </button>
              <button
                type="button"
                className={`btn btn-sm ${apagarArmado ? 'btn-danger' : 'btn-ghost'}`}
                onClick={() => void removerDestaque()}
              >
                {apagarArmado ? 'Confirmar apagar' : 'Apagar tinta'}
              </button>
            </div>
          </div>
        )}
      </div>
      <p className="pagina-numero">{pagina.numero}</p>
    </div>
  )
}
