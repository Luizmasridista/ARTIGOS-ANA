import type { Citacao } from '../api'
import { IconeLink, IconeSetaDireita } from './Icones'

interface Props {
  citacoes: Citacao[] | null
  carregando: boolean
  erro: string | null
  enriquecendoId: number | null
  onEnriquecer: (citacao: Citacao) => void
  onAbrirExterno: (url: string) => void
  onTentarNovamente?: () => void
  onIrParaFonte?: (citacao: Citacao) => void
}

function temOcorrencia(c: Citacao): boolean {
  return typeof c.pagina === 'number' && c.pagina > 0 && Array.isArray(c.pos) && c.pos !== null
}

const TIPO_LABEL: Record<string, string> = {
  autor_ano: 'Autor-data',
  numerica: 'Numérica',
  referencia: 'Referência',
}

function grupoTipoLabel(tipo: string): string {
  return TIPO_LABEL[tipo] ?? tipo
}

function agruparPorTipo(citacoes: Citacao[]): Map<string, Citacao[]> {
  const map = new Map<string, Citacao[]>()
  const ordem = ['autor_ano', 'numerica', 'referencia']
  for (const c of citacoes) {
    const arr = map.get(c.tipo) ?? []
    arr.push(c)
    map.set(c.tipo, arr)
  }
  // ordena chaves pela ordem conhecida, depois resto
  const sorted = new Map<string, Citacao[]>()
  for (const k of ordem) {
    if (map.has(k)) sorted.set(k, map.get(k)!)
  }
  for (const [k, v] of map) {
    if (!sorted.has(k)) sorted.set(k, v)
  }
  return sorted
}

export function CitacoesTab({ citacoes, carregando, erro, enriquecendoId, onEnriquecer, onAbrirExterno, onTentarNovamente, onIrParaFonte }: Props) {
  if (carregando) {
    return (
      <div className="painel-conteudo">
        <div className="estado-vazio">
          <p style={{ fontSize: 'var(--fs-sm)' }}>Carregando fontes…</p>
          <div className="progresso" style={{ width: '100%', maxWidth: 180 }}>
            <div className="progresso-barra" />
          </div>
        </div>
      </div>
    )
  }

  if (erro) {
    return (
      <div className="painel-conteudo">
        <div className="estado-vazio">
          <p style={{ fontSize: 'var(--fs-sm)' }}>{erro}</p>
          {onTentarNovamente && (
            <button type="button" className="btn btn-sm" onClick={onTentarNovamente} style={{ marginTop: 8 }}>
              Tentar novamente
            </button>
          )}
        </div>
      </div>
    )
  }

  if (!citacoes || citacoes.length === 0) {
    return (
      <div className="painel-conteudo">
        <div className="estado-vazio">
          <p style={{ fontSize: 'var(--fs-sm)' }}>Nenhuma citação encontrada neste artigo.</p>
        </div>
      </div>
    )
  }

  const grupos = agruparPorTipo(citacoes)

  return (
    <div className="painel-conteudo citacoes-conteudo">
      {Array.from(grupos.entries()).map(([tipo, lista]) => (
        <div key={tipo} className="citacao-grupo">
          <h3 className="citacao-grupo-titulo">
            {grupoTipoLabel(tipo)} <span className="citacao-grupo-qtd">{lista.length}</span>
          </h3>
          <div className="citacao-lista">
            {lista.map((c) => {
              const navegavel = temOcorrencia(c)
              const paginaLabel = navegavel ? `Página ${c.pagina}` : null
              const ocorrenciasTotal = c.ocorrencias?.length ?? 0
              return (
                <div
                  key={c.id}
                  className={`citacao-card${navegavel ? ' citacao-card--clicavel' : ' citacao-card--sem-pos'}`}
                  role={navegavel ? 'button' : undefined}
                  tabIndex={navegavel ? 0 : undefined}
                  aria-label={navegavel ? `Ver no texto página ${c.pagina}` : undefined}
                  title={navegavel ? `Ver no texto página ${c.pagina}` : 'Sem ocorrência no corpo do texto'}
                  onClick={() => {
                    if (navegavel) onIrParaFonte?.(c)
                    else onIrParaFonte?.(c)
                  }}
                  onKeyDown={(e) => {
                    if (!navegavel) return
                    if (e.key === 'Enter' || e.key === ' ') {
                      e.preventDefault()
                      onIrParaFonte?.(c)
                    }
                  }}
                >
                  <div className="citacao-card-topo">
                    <span className="citacao-card-chave" title={c.chave}>
                      {c.chave}
                    </span>
                    {c.tipo && <span className="citacao-tipo">{c.tipo}</span>}
                  </div>
                  {(c.autor || c.ano) && (
                    <p className="citacao-card-meta">
                      {[c.autor, c.ano ? String(c.ano) : null].filter(Boolean).join(' · ')}
                    </p>
                  )}
                  {c.titulo && <p className="citacao-card-titulo">{c.titulo}</p>}
                  {c.trecho && <p className="citacao-card-trecho">"{c.trecho}"</p>}
                  {c.texto && !c.trecho && <p className="citacao-card-trecho">{c.texto}</p>}
                  {(paginaLabel || ocorrenciasTotal > 1) && (
                    <div className="citacao-card-pagina-linha">
                      {paginaLabel && <span className="citacao-pagina-badge">{paginaLabel}</span>}
                      {ocorrenciasTotal > 1 && (
                        <span className="citacao-ocorrencias-badge" title={`${ocorrenciasTotal} ocorrências no texto`}>
                          {ocorrenciasTotal} ocorrências
                        </span>
                      )}
                    </div>
                  )}
                  <div className="citacao-card-acoes">
                    <button
                      type="button"
                      className="btn btn-sm"
                      disabled={!navegavel}
                      title={navegavel ? `Ver no texto página ${c.pagina}` : 'Sem ocorrência no corpo do texto'}
                      aria-label={navegavel ? `Ver no texto página ${c.pagina}` : 'Sem ocorrência no corpo do texto'}
                      onClick={(e) => {
                        e.stopPropagation()
                        onIrParaFonte?.(c)
                      }}
                    >
                      <IconeSetaDireita size={13} />
                      Ver no texto
                    </button>
                    {c.url ? (
                      <button
                        type="button"
                        className="btn btn-sm citacao-link"
                        onClick={(e) => {
                          e.stopPropagation()
                          onAbrirExterno(c.url)
                        }}
                        title={c.url}
                      >
                        <IconeLink size={13} />
                        Abrir fonte
                      </button>
                    ) : (
                      <button
                        type="button"
                        className="btn btn-sm"
                        onClick={(e) => {
                          e.stopPropagation()
                          onEnriquecer(c)
                        }}
                        disabled={enriquecendoId === c.id}
                      >
                        <IconeLink size={13} />
                        {enriquecendoId === c.id ? 'Buscando…' : 'Buscar fonte'}
                      </button>
                    )}
                  </div>
                  {c.url && (
                    <a
                      className="citacao-url"
                      href={c.url}
                      onClick={(e) => {
                        e.preventDefault()
                        e.stopPropagation()
                        onAbrirExterno(c.url)
                      }}
                      title={c.url}
                    >
                      {c.url}
                    </a>
                  )}
                </div>
              )
            })}
          </div>
        </div>
      ))}
    </div>
  )
}
