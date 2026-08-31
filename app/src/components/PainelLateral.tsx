import { lazy, Suspense, useEffect, useRef, useState } from 'react'
import type { Citacao, HistoricoEvento, Nota, SumarioItem } from '../api'
import { fmtData } from './Biblioteca'
import { IconeHistorico, IconeLink, IconeLixeira, IconeLivro, IconeNota } from './Icones'

const CitacoesTab = lazy(() => import('./CitacoesTab').then((m) => ({ default: m.CitacoesTab })))

export type AbaPainel = 'notas' | 'sumario' | 'citacoes' | 'historico'

interface Props {
  numPaginas?: number
  notas: Nota[]
  historico: HistoricoEvento[] | null
  historicoErro: boolean
  aba: AbaPainel
  onMudarAba: (aba: AbaPainel) => void
  onCriarNota?: (pagina: number, texto: string, marcacaoId?: number, tags?: string[], cor?: string) => Promise<boolean>
  onRemoverNota: (nota: Nota) => Promise<void>
  onIrParaPagina: (numero: number) => void
  prefill?: { texto: string; pagina: number; marcacaoId?: number } | null
  prefillNonce?: number
  removendoId: number | null
  notaFoco: { id: number; nonce: number } | null
  numeroDaNota: (notaId: number) => number
  aberto: boolean
  citacoes: Citacao[] | null
  citacoesCarregando: boolean
  citacoesErro: string | null
  enriquecendoId: number | null
  onEnriquecer: (citacao: Citacao) => void
  onAbrirExterno: (url: string) => void
  onRecarregarCitacoes?: () => void
  onIrParaFonte?: (citacao: Citacao) => void
  sumario: SumarioItem[] | null
  sumarioCarregando: boolean
  sumarioErro: string | null
  onFechar?: () => void
}

function clampLargura(v: number): number {
  return Math.min(560, Math.max(260, Math.round(v)))
}

export function fmtDataHora(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleString('pt-BR', {
    day: '2-digit',
    month: '2-digit',
    year: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

export function PainelLateral({
  notas,
  historico,
  historicoErro,
  aba,
  onMudarAba,
  onRemoverNota,
  onIrParaPagina,
  removendoId,
  notaFoco,
  aberto,
  citacoes,
  citacoesCarregando,
  citacoesErro,
  enriquecendoId,
  onEnriquecer,
  onAbrirExterno,
  onRecarregarCitacoes,
  onIrParaFonte,
  sumario,
  sumarioCarregando,
  sumarioErro,
  onFechar,
}: Props) {
  const [armadoId, setArmadoId] = useState<number | null>(null)
  const [largura, setLargura] = useState(() => clampLargura(Number(localStorage.getItem('painel-largura')) || 320))
  const [filtroTag, setFiltroTag] = useState<string | null>(null)
  const [filtroCor, setFiltroCor] = useState<string | null>(null)
  const armadoTimer = useRef<number | null>(null)
  const painelRef = useRef<HTMLElement>(null)
  const touchStart = useRef<{ x: number; y: number } | null>(null)

  useEffect(() => {
    if (!notaFoco || notaFoco.nonce === 0) return
    const el = document.querySelector<HTMLElement>(`[data-nota-id="${notaFoco.id}"]`)
    if (!el) return
    el.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    el.classList.add('focada')
    const t = window.setTimeout(() => el.classList.remove('focada'), 1800)
    return () => window.clearTimeout(t)
  }, [notaFoco])

  // ESC fecha gaveta quando aberta (iPad drawer)
  useEffect(() => {
    if (!aberto || !onFechar) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') onFechar()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [aberto, onFechar])

  const iniciarResizeLargura = (e: React.MouseEvent) => {
    e.preventDefault()
    const inicioX = e.clientX
    const inicioL = largura
    const mover = (ev: MouseEvent) => {
      const nova = clampLargura(inicioL + (inicioX - ev.clientX))
      setLargura(nova)
      localStorage.setItem('painel-largura', String(nova))
    }
    const soltar = () => {
      window.removeEventListener('mousemove', mover)
      window.removeEventListener('mouseup', soltar)
    }
    window.addEventListener('mousemove', mover)
    window.addEventListener('mouseup', soltar)
  }

  const pedirExclusao = (nota: Nota) => {
    if (armadoId === nota.id) {
      setArmadoId(null)
      if (armadoTimer.current) window.clearTimeout(armadoTimer.current)
      void onRemoverNota(nota)
      return
    }
    setArmadoId(nota.id)
    if (armadoTimer.current) window.clearTimeout(armadoTimer.current)
    armadoTimer.current = window.setTimeout(() => setArmadoId(null), 3000)
  }

  const ordenadas = [...notas].sort((a, b) => a.id - b.id)

  // filtros
  const todasTags = Array.from(new Set(ordenadas.flatMap((n) => n.tags ?? []))).sort()
  const todasCores = Array.from(new Set(ordenadas.map((n) => n.cor).filter(Boolean))).sort()

  // limpa filtro se valor sumiu
  useEffect(() => {
    if (filtroTag && !todasTags.includes(filtroTag)) setFiltroTag(null)
  }, [filtroTag, todasTags])
  useEffect(() => {
    if (filtroCor && !todasCores.includes(filtroCor)) setFiltroCor(null)
  }, [filtroCor, todasCores])

  const filtradas = ordenadas.filter((n) => {
    if (filtroTag && !(n.tags ?? []).includes(filtroTag)) return false
    if (filtroCor && n.cor !== filtroCor) return false
    return true
  })

  const handleTouchStart = (e: React.TouchEvent) => {
    touchStart.current = { x: e.touches[0].clientX, y: e.touches[0].clientY }
  }
  const handleTouchEnd = (e: React.TouchEvent) => {
    if (!touchStart.current || !onFechar || !aberto) return
    const dx = e.changedTouches[0].clientX - touchStart.current.x
    const dy = e.changedTouches[0].clientY - touchStart.current.y
    // swipe para direita (>80px horizontal e mais horizontal que vertical) fecha
    if (dx > 80 && Math.abs(dx) > Math.abs(dy) * 1.2) {
      onFechar()
    }
    touchStart.current = null
  }

  return (
    <>
      <button
        type="button"
        className={`painel-backdrop${aberto ? '' : ' fechado'}`}
        aria-label="Fechar painel lateral"
        aria-hidden={!aberto}
        tabIndex={aberto ? 0 : -1}
        onClick={() => onFechar?.()}
      />
      <aside
        ref={painelRef}
        className={`painel${aberto ? '' : ' fechado'}`}
        style={{ width: largura }}
        onTouchStart={handleTouchStart}
        onTouchEnd={handleTouchEnd}
      >
      <div
        className="painel-resizer"
        role="separator"
        aria-orientation="vertical"
        title="Arraste para ajustar a largura"
        onMouseDown={iniciarResizeLargura}
      />
      <div className="painel-tabs" role="tablist">
        <button
          type="button"
          role="tab"
          aria-selected={aba === 'notas'}
          className={`painel-tab ${aba === 'notas' ? 'ativa' : ''}`}
          onClick={() => onMudarAba('notas')}
        >
          <IconeNota size={14} />
          Notas
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={aba === 'sumario'}
          className={`painel-tab ${aba === 'sumario' ? 'ativa' : ''}`}
          onClick={() => onMudarAba('sumario')}
        >
          <IconeLivro size={14} />
          Sumário
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={aba === 'citacoes'}
          className={`painel-tab ${aba === 'citacoes' ? 'ativa' : ''}`}
          onClick={() => onMudarAba('citacoes')}
        >
          <IconeLink size={14} />
          Fontes
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={aba === 'historico'}
          className={`painel-tab ${aba === 'historico' ? 'ativa' : ''}`}
          onClick={() => onMudarAba('historico')}
        >
          <IconeHistorico size={14} />
          Histórico
        </button>
      </div>

      {aba === 'notas' && (
        <>
          {(todasTags.length > 0 || todasCores.length > 0) && (
            <div className="painel-filtros">
              {todasTags.length > 0 && (
                <div className="filtro-grupo">
                  <span className="filtro-label">Tag:</span>
                  <div className="filtro-chips">
                    {todasTags.map((tag) => (
                      <button
                        key={tag}
                        type="button"
                        className={`chip-tag ${filtroTag === tag ? 'ativa' : ''}`}
                        onClick={() => setFiltroTag((v) => (v === tag ? null : tag))}
                        title={`Filtrar por ${tag}`}
                      >
                        {tag}
                      </button>
                    ))}
                  </div>
                </div>
              )}
              {todasCores.length > 0 && (
                <div className="filtro-grupo">
                  <span className="filtro-label">Cor:</span>
                  <div className="filtro-cores">
                    {todasCores.map((cor) => (
                      <button
                        key={cor}
                        type="button"
                        className={`chip-cor ${filtroCor === cor ? 'ativa' : ''}`}
                        style={{ background: cor }}
                        onClick={() => setFiltroCor((v) => (v === cor ? null : cor))}
                        title={cor}
                        aria-label={`Filtrar por cor ${cor}`}
                      />
                    ))}
                  </div>
                </div>
              )}
              {(filtroTag || filtroCor) && (
                <div className="filtro-contador">
                  <span>
                    {filtradas.length} de {ordenadas.length} notas
                  </span>
                  <button type="button" className="btn btn-sm btn-ghost" onClick={() => { setFiltroTag(null); setFiltroCor(null) }}>
                    Limpar
                  </button>
                </div>
              )}
            </div>
          )}
          <div className="painel-conteudo">
            {ordenadas.length === 0 && (
              <div className="estado-vazio">
                <p style={{ fontSize: 'var(--fs-sm)' }}>
                  Nenhuma nota ainda.
                  <br />
                  Selecione um trecho e clique em "Nota".
                </p>
              </div>
            )}
            {ordenadas.length > 0 && filtradas.length === 0 && (
              <div className="estado-vazio">
                <p style={{ fontSize: 'var(--fs-sm)' }}>Nenhuma nota para os filtros selecionados.</p>
                <button type="button" className="btn btn-sm" onClick={() => { setFiltroTag(null); setFiltroCor(null) }}>
                  Limpar filtros
                </button>
              </div>
            )}
            {filtradas.map((nota) => (
              <div key={nota.id} className="nota-card nota-card--minimal" data-nota-id={nota.id}>
                <div className="nota-card-cabecalho-minimal">
                  <button
                    type="button"
                    className="nota-card-ir"
                    onClick={() => onIrParaPagina(nota.pagina)}
                    title={`Ir para a marcação na página ${nota.pagina}`}
                    aria-label={`Ir para a marcação na página ${nota.pagina}`}
                  >
                    <IconeLink size={12} />
                    Ir para página {nota.pagina}
                  </button>
                  <span className="nota-card-data-minimal">{fmtData(nota.criado_em)}</span>
                  {nota.cor && <span className="nota-cor-ponto" style={{ background: nota.cor }} aria-hidden="true" />}
                </div>
                <p className="nota-card-texto-minimal">{nota.texto}</p>
                {(nota.tags ?? []).length > 0 && (
                  <div className="nota-tags-minimal">
                    {(nota.tags ?? []).map((tag) => (
                      <span key={tag} className="nota-tag-minimal">
                        #{tag}
                      </span>
                    ))}
                  </div>
                )}
                <div className="nota-card-acoes-minimal">
                  <button
                    type="button"
                    className={`nota-excluir ${armadoId === nota.id ? 'nota-excluir--armado' : ''}`}
                    onClick={() => pedirExclusao(nota)}
                    disabled={removendoId === nota.id}
                    aria-label={armadoId === nota.id ? 'Confirmar exclusão' : 'Excluir nota'}
                  >
                    <IconeLixeira size={12} />
                    {armadoId === nota.id ? 'Confirmar' : 'Excluir'}
                  </button>
                </div>
              </div>
            ))}
          </div>
        </>
      )}

      {aba === 'sumario' && (
        <div className="painel-conteudo">
          {sumarioCarregando && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>Carregando sumário…</p>
              <div className="progresso" style={{ width: '100%', maxWidth: 180 }}>
                <div className="progresso-barra" />
              </div>
            </div>
          )}
          {sumarioErro && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>{sumarioErro}</p>
            </div>
          )}
          {!sumarioCarregando && !sumarioErro && sumario && sumario.length === 0 && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>Nenhuma seção detectada.</p>
            </div>
          )}
          {!sumarioCarregando && !sumarioErro && sumario && sumario.length > 0 && (
            <div className="sumario-lista">
              {sumario.map((item) => (
                <button
                  key={item.ordem}
                  type="button"
                  className="sumario-item"
                  onClick={() => onIrParaPagina(item.pagina)}
                  title={`Ir para página ${item.pagina}`}
                >
                  <span className="sumario-titulo">{item.titulo}</span>
                  <span className="sumario-pagina">p. {item.pagina}</span>
                </button>
              ))}
            </div>
          )}
        </div>
      )}

      {aba === 'citacoes' && (
        <Suspense fallback={<div className="painel-conteudo"><div className="estado-vazio"><p style={{ fontSize: 'var(--fs-sm)' }}>Carregando fontes…</p></div></div>}>
          <CitacoesTab
            citacoes={citacoes}
            carregando={citacoesCarregando}
            erro={citacoesErro}
            enriquecendoId={enriquecendoId}
            onEnriquecer={onEnriquecer}
            onAbrirExterno={onAbrirExterno}
            onTentarNovamente={onRecarregarCitacoes}
            onIrParaFonte={onIrParaFonte}
          />
        </Suspense>
      )}

      {aba === 'historico' && (
        <div className="painel-conteudo">
          {historico === null && !historicoErro && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>Carregando histórico…</p>
            </div>
          )}
          {historicoErro && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>Histórico indisponível no momento.</p>
            </div>
          )}
          {historico && historico.length === 0 && (
            <div className="estado-vazio">
              <p style={{ fontSize: 'var(--fs-sm)' }}>Nenhum evento registrado.</p>
            </div>
          )}
          {historico?.map((evento) => (
            <div key={evento.id} className="historico-item">
              <div className="historico-item-topo">
                <span className="historico-item-entidade">{evento.entidade}</span>
                <span className="historico-item-acao">{evento.acao}</span>
              </div>
              <span className="historico-item-data">{fmtDataHora(evento.criado_em)}</span>
              {typeof evento.dados === 'object' && evento.dados !== null && (
                <span className="historico-item-dados">{JSON.stringify(evento.dados)}</span>
              )}
            </div>
          ))}
        </div>
      )}
      </aside>
    </>
  )
}
