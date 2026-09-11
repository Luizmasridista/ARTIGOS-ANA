import { lazy, Suspense, useCallback, useEffect, useRef, useState } from 'react'
import { api, type ArtigoResumo } from '../api'
import { DialogoConfirmacao } from './DialogoConfirmacao'
import { IconeArquivo, IconeBusca, IconeLixeira, IconeUpload } from './Icones'
import { useAuth } from '../context/AuthContext'
import { cacheArtigos, loadArtigos } from '../offline/cache'

function isNetworkError(e: unknown): boolean {
  if (e instanceof TypeError) return true
  const msg = e instanceof Error ? e.message : String(e)
  return /Failed to fetch|NetworkError|network|Load failed/i.test(msg)
}

// aguardarJob espera o worker concluir o processamento do PDF (polling).
// Lança erro 'job-falhou' se o processamento falhar, 'job-tempo' se estourar 12min.
async function aguardarJob(jobId: number): Promise<void> {
  const limite = Date.now() + 12 * 60 * 1000
  for (;;) {
    const job = await api.getJob(jobId)
    if (job.status === 'done') return
    if (job.status === 'failed') throw new Error('job-falhou')
    if (Date.now() > limite) throw new Error('job-tempo')
    await new Promise((r) => setTimeout(r, 3000))
  }
}

const GrokBanner = lazy(() => import('./GrokBanner').then((m) => ({ default: m.GrokBanner })))

interface Props {
  onAbrirArtigo: (artigo: ArtigoResumo) => void
}

export function fmtData(iso: string): string {
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  return d.toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit', year: 'numeric' })
}

function escapeRegExp(s: string): string {
  return s.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function highlightPartes(texto: string, busca: string): Array<{ texto: string; destaque: boolean }> {
  const q = busca.trim()
  if (!q) return [{ texto, destaque: false }]
  const esc = escapeRegExp(q)
  const re = new RegExp(`(${esc})`, 'gi')
  const parts = texto.split(re)
  const lowerQ = q.toLowerCase()
  return parts
    .filter((p) => p !== '')
    .map((p) => ({ texto: p, destaque: p.toLowerCase() === lowerQ }))
}

export function HighlightTitulo({ titulo, busca }: { titulo: string; busca: string }) {
  const partes = highlightPartes(titulo, busca)
  // if no highlight needed, render plain
  const temDestaque = partes.some((p) => p.destaque)
  if (!temDestaque) return <>{titulo}</>
  return (
    <>
      {partes.map((p, i) =>
        p.destaque ? (
          <mark key={i} className="busca-mark">
            {p.texto}
          </mark>
        ) : (
          <span key={i}>{p.texto}</span>
        ),
      )}
    </>
  )
}

export function Biblioteca({ onAbrirArtigo }: Props) {
  const [artigos, setArtigos] = useState<ArtigoResumo[]>([])
  const [carregando, setCarregando] = useState(true)
  const [erro, setErro] = useState<string | null>(null)
  const [busca, setBusca] = useState('')
  const [buscaDebounced, setBuscaDebounced] = useState('')
  const [arrastando, setArrastando] = useState(false)
  const [enviando, setEnviando] = useState<string | null>(null)
  const [erroEnvio, setErroEnvio] = useState<string | null>(null)
  const [excluirAlvo, setExcluirAlvo] = useState<ArtigoResumo | null>(null)
  const [excluindo, setExcluindo] = useState(false)
  const [loteAberto, setLoteAberto] = useState(false)
  const [selecionados, setSelecionados] = useState<Set<number>>(new Set())
  const [loteConfirmando, setLoteConfirmando] = useState(false)
  const [loteExcluindo, setLoteExcluindo] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)
  const loteRef = useRef<HTMLDivElement>(null)
  const { user } = useAuth()
  const [avisoOffline, setAvisoOffline] = useState<string | null>(null)

  // debounce busca -> buscaDebounced
  useEffect(() => {
    const t = window.setTimeout(() => setBuscaDebounced(busca), 300)
    return () => window.clearTimeout(t)
  }, [busca])

  const carregar = useCallback(async (termo?: string) => {
    setCarregando(true)
    setErro(null)
    setAvisoOffline(null)
    try {
      const q = termo !== undefined ? termo : buscaDebounced
      const lista = await api.listarArtigos(q)
      setArtigos(lista)
      if (user?.id) {
        try { await cacheArtigos(user.id, lista) } catch {}
      }
    } catch (e) {
      if (isNetworkError(e) && user?.id) {
        try {
          const cached = await loadArtigos(user.id)
          const q = (termo !== undefined ? termo : buscaDebounced).trim().toLowerCase()
          const filtrados = q ? cached.filter(a => a.titulo.toLowerCase().includes(q)) : cached
          if (filtrados.length > 0) {
            setArtigos(filtrados)
            setAvisoOffline('Sem conexão — mostrando artigos salvos neste aparelho')
            setErro(null)
          } else if (cached.length === 0) {
            setArtigos([])
            setErro('Sem conexão e sem cache local para este usuário')
          } else {
            setArtigos([])
            setErro(null)
          }
        } catch {
          setErro(e instanceof Error ? e.message : 'Falha ao carregar os artigos')
        }
      } else {
        setErro(e instanceof Error ? e.message : 'Falha ao carregar os artigos')
      }
    } finally {
      setCarregando(false)
    }
  }, [buscaDebounced, user?.id])

  useEffect(() => {
    void carregar(buscaDebounced)
  }, [buscaDebounced, carregar])

  // recarregar sem busca ao montar (carregar já cobre com buscaDebounced inicial '')
  // mas garantir primeira carga sem debounce já foi tratada acima

  const enviarArquivos = useCallback(
    async (files: FileList | null) => {
      if (!files || files.length === 0) return
      if (!navigator.onLine) {
        setErroEnvio('Sem conexão — envie o PDF quando estiver online. PDFs ainda são processados apenas no servidor.')
        return
      }
      const file = Array.from(files).find(
        (f) => f.type === 'application/pdf' || f.name.toLowerCase().endsWith('.pdf'),
      )
      setErroEnvio(null)
      if (!file) {
        setErroEnvio('Escolha um arquivo PDF.')
        return
      }
      setEnviando(file.name)
      try {
        const criado = await api.criarArtigo(file)
        // Upload é assíncrono: PDF grande processa no worker (senão estoura
        // o timeout do proxy e o upload "trava"). Aguarda o job concluir.
        if (criado.job_id && criado.status === 'processando') {
          setEnviando(`Processando ${file.name}…`)
          await aguardarJob(criado.job_id)
          const detalhe = await api.getArtigo(criado.id)
          const pronto = {
            id: criado.id,
            titulo: detalhe.titulo,
            num_paginas: detalhe.paginas.length,
            criado_em: criado.criado_em,
          }
          setEnviando(null)
          if (user?.id) { try { await cacheArtigos(user.id, [pronto]) } catch {} }
          await carregar(buscaDebounced)
          onAbrirArtigo(pronto)
          return
        }
        setEnviando(null)
        if (user?.id) { try { await cacheArtigos(user.id, [criado]) } catch {} }
        await carregar(buscaDebounced)
        onAbrirArtigo(criado)
      } catch (e) {
        setEnviando(null)
        if (isNetworkError(e)) setErroEnvio('Sem conexão — PDF não enviado. Tente quando voltar a ficar online.')
        else if (e instanceof Error && e.message === 'job-falhou') setErroEnvio('Falha ao processar o PDF. Tente de novo ou use um arquivo menor.')
        else if (e instanceof Error && e.message === 'job-tempo') setErroEnvio('Processamento demorou demais. Verifique a biblioteca em instantes.')
        else setErroEnvio(e instanceof Error ? e.message : 'Falha ao enviar o PDF')
      }
    },
    [carregar, onAbrirArtigo, buscaDebounced, user?.id],
  )

  const confirmarExclusao = async () => {
    if (!excluirAlvo) return
    if (!navigator.onLine) {
      setErro('Sem conexão — exclusão aguarda reconexão. Nada foi excluído.')
      setExcluirAlvo(null)
      return
    }
    setExcluindo(true)
    try {
      await api.deletarArtigo(excluirAlvo.id)
      setExcluirAlvo(null)
      await carregar(buscaDebounced)
    } catch (e) {
      setExcluirAlvo(null)
      if (isNetworkError(e)) setErro('Sem conexão — exclusão não concluída. Tente novamente quando online.')
      else setErro(e instanceof Error ? e.message : 'Falha ao excluir o artigo')
    } finally {
      setExcluindo(false)
    }
  }

  // artigos já vem filtrado do servidor conforme buscaDebounced
  const filtrados = artigos

  const toggleLote = useCallback(() => {
    if (loteAberto) {
      setLoteAberto(false)
      return
    }
    setSelecionados(new Set(filtrados.map((a) => a.id)))
    setLoteAberto(true)
  }, [loteAberto, filtrados])

  useEffect(() => {
    if (!loteAberto || loteConfirmando) return
    const onDown = (e: MouseEvent) => {
      if (loteRef.current && !loteRef.current.contains(e.target as Node)) {
        setLoteAberto(false)
      }
    }
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setLoteAberto(false)
    }
    document.addEventListener('mousedown', onDown)
    document.addEventListener('keydown', onKey)
    return () => {
      document.removeEventListener('mousedown', onDown)
      document.removeEventListener('keydown', onKey)
    }
  }, [loteAberto, loteConfirmando])

  const confirmarLote = async () => {
    const ids = Array.from(selecionados)
    if (ids.length === 0) return
    if (!navigator.onLine) {
      setLoteConfirmando(false)
      setErro('Sem conexão — exclusão em lote aguarda reconexão.')
      return
    }
    setLoteExcluindo(true)
    try {
      await api.excluirLote(ids)
      setLoteConfirmando(false)
      setLoteAberto(false)
      setSelecionados(new Set())
      await carregar(buscaDebounced)
    } catch (e) {
      setLoteConfirmando(false)
      if (isNetworkError(e)) setErro('Sem conexão — exclusão em lote não concluída.')
      else setErro(e instanceof Error ? e.message : 'Falha ao excluir em lote')
    } finally {
      setLoteExcluindo(false)
    }
  }

  const contadorTexto = (() => {
    const q = buscaDebounced.trim()
    if (q) {
      const n = filtrados.length
      return `${n} ${n === 1 ? 'resultado' : 'resultados'} para "${q}"`
    }
    // sem busca: mostra total
    if (!carregando && !erro && filtrados.length > 0) {
      return `${filtrados.length} ${filtrados.length === 1 ? 'artigo' : 'artigos'}`
    }
    return null
  })()

  return (
    <div
      className="biblioteca"
      onDragOver={(e) => {
        e.preventDefault()
        setArrastando(true)
      }}
      onDragLeave={(e) => {
        if (e.currentTarget === e.target) setArrastando(false)
      }}
      onDrop={(e) => {
        e.preventDefault()
        setArrastando(false)
        void enviarArquivos(e.dataTransfer.files)
      }}
    >
      <div className="biblioteca-inner">
        <Suspense fallback={null}>
          <GrokBanner />
        </Suspense>
        <div className="biblioteca-topo">
          <div>
            <h1 className="biblioteca-titulo">Biblioteca</h1>
            <p className="biblioteca-subtitulo">Seus artigos e anotações em um só lugar.</p>
          </div>
          <div className="biblioteca-acoes">
            {filtrados.length > 0 && (
              <div className="lote-wrapper" ref={loteRef}>
                <button
                  type="button"
                  className="btn"
                  onClick={toggleLote}
                  aria-expanded={loteAberto}
                  aria-haspopup="dialog"
                  aria-label="Excluir em lote"
                >
                  <IconeLixeira size={15} />
                  Excluir em lote
                </button>
                {loteAberto && (
                  <div className="lote-dropdown" role="dialog" aria-modal="false" aria-label="Excluir em lote">
                    <div className="lote-dropdown-topo">
                      <span className="lote-dropdown-titulo">Escolha o que excluir</span>
                      <span className="lote-dropdown-contador">{selecionados.size} selecionados</span>
                    </div>
                    <p className="lote-dropdown-dica">Marcado = será excluído. Desmarque o que deseja manter.</p>
                    <div className="lote-lista">
                      {filtrados.map((a) => {
                        const checked = selecionados.has(a.id)
                        return (
                          <label key={a.id} className="lote-item">
                            <input
                              type="checkbox"
                              checked={checked}
                              onChange={(e) => {
                                setSelecionados((prev) => {
                                  const next = new Set(prev)
                                  if (e.target.checked) next.add(a.id)
                                  else next.delete(a.id)
                                  return next
                                })
                              }}
                            />
                            <span className="lote-item-titulo" title={a.titulo}>
                              {a.titulo}
                            </span>
                          </label>
                        )
                      })}
                    </div>
                    <div className="lote-acoes">
                      <button type="button" className="btn btn-sm" onClick={() => setLoteAberto(false)}>
                        Cancelar
                      </button>
                      <button
                        type="button"
                        className="btn btn-sm btn-danger"
                        disabled={selecionados.size === 0 || loteExcluindo}
                        onClick={() => setLoteConfirmando(true)}
                      >
                        Excluir selecionados
                      </button>
                    </div>
                  </div>
                )}
              </div>
            )}
            <div className="biblioteca-busca">
              <div style={{ position: 'relative' }}>
                <span
                  style={{
                    position: 'absolute',
                    left: 10,
                    top: '50%',
                    transform: 'translateY(-50%)',
                    color: 'var(--muted)',
                    display: 'grid',
                    placeItems: 'center',
                  }}
                >
                  <IconeBusca size={15} />
                </span>
                <input
                  className="input"
                  style={{ width: '100%', paddingLeft: 32 }}
                  type="search"
                  placeholder="Buscar por título, nota ou citação"
                  value={busca}
                  onChange={(e) => setBusca(e.target.value)}
                  aria-label="Buscar artigo"
                />
              </div>
            </div>
          </div>
        </div>

        {avisoOffline && !carregando && (
          <div className="aviso" role="status" aria-live="polite">
            <span>{avisoOffline}</span>
          </div>
        )}
        {contadorTexto && !carregando && !erro && (
          <div className="biblioteca-contador" role="status" aria-live="polite">
            {contadorTexto}
          </div>
        )}

        <input
          ref={inputRef}
          type="file"
          accept="application/pdf,.pdf"
          hidden
          onChange={(e) => {
            void enviarArquivos(e.target.files)
            e.target.value = ''
          }}
        />

        {erroEnvio && (
          <div className="aviso">
            <span>{erroEnvio}</span>
          </div>
        )}

        {enviando && (
          <div className="uploadando">
            <div className="progresso" style={{ flex: 1 }}>
              <div className="progresso-barra" />
            </div>
            <span style={{ color: 'var(--text-2)', fontSize: 'var(--fs-sm)' }}>
              Enviando {enviando}…
            </span>
          </div>
        )}

        {carregando && (
          <div className="estado-vazio">
            <p>Carregando biblioteca…</p>
          </div>
        )}

        {!carregando && erro && (
          <div className="estado-vazio">
            <p>{erro}</p>
            <button type="button" className="btn" onClick={() => void carregar(buscaDebounced)}>
              Tentar novamente
            </button>
          </div>
        )}

        {!carregando && !erro && artigos.length === 0 && buscaDebounced.trim() === '' && (
          <button
            type="button"
            className={`dropzone hero ${arrastando ? 'ativo' : ''}`}
            onClick={() => inputRef.current?.click()}
          >
            <span className="dropzone-icone">
              <IconeUpload size={28} />
            </span>
            <p className="dropzone-titulo">Arraste um PDF aqui ou clique para escolher</p>
            <p className="dropzone-dica">O artigo é extraído e aberto em segundos.</p>
          </button>
        )}

        {!carregando && !erro && (artigos.length > 0 || buscaDebounced.trim() !== '') && (
          <>
            {buscaDebounced.trim() === '' && (
              <button
                type="button"
                className={`dropzone ${arrastando ? 'ativo' : ''}`}
                style={{ padding: 'var(--sp-4)' }}
                onClick={() => inputRef.current?.click()}
              >
                <p className="dropzone-dica" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                  <IconeUpload size={16} />
                  Arraste um PDF aqui ou clique para adicionar outro artigo
                </p>
              </button>
            )}

            {filtrados.length === 0 ? (
              <div className="estado-vazio">
                <p>
                  Nenhum artigo encontrado para "<span className="busca-termo">{buscaDebounced.trim()}</span>".
                </p>
              </div>
            ) : (
              <div className="lista-artigos">
                {filtrados.map((artigo) => (
                  <div
                    key={artigo.id}
                    className="artigo-card"
                    role="button"
                    tabIndex={0}
                    onClick={() => onAbrirArtigo(artigo)}
                    onKeyDown={(e) => {
                      if (e.key === 'Enter') onAbrirArtigo(artigo)
                    }}
                  >
                    <span className="artigo-card-icone">
                      <IconeArquivo size={20} />
                    </span>
                    <div className="artigo-card-info">
                      <p className="artigo-card-titulo">
                        <HighlightTitulo titulo={artigo.titulo} busca={buscaDebounced} />
                      </p>
                      <p className="artigo-card-meta">
                        {artigo.num_paginas} {artigo.num_paginas === 1 ? 'página' : 'páginas'} ·{' '}
                        {fmtData(artigo.criado_em)}
                      </p>
                    </div>
                    <button
                      type="button"
                      className="btn btn-icon artigo-card-acao"
                      title="Excluir artigo"
                      aria-label={`Excluir ${artigo.titulo}`}
                      onClick={(e) => {
                        e.stopPropagation()
                        setExcluirAlvo(artigo)
                      }}
                    >
                      <IconeLixeira size={15} />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </>
        )}
      </div>

      {excluirAlvo && (
        <DialogoConfirmacao
          titulo="Excluir artigo"
          mensagem={`Excluir "${excluirAlvo.titulo}"? As marcações e notas deste artigo também serão removidas.`}
          confirmarLabel="Excluir"
          carregando={excluindo}
          onConfirmar={() => void confirmarExclusao()}
          onCancelar={() => setExcluirAlvo(null)}
        />
      )}

      {loteConfirmando && (
        <DialogoConfirmacao
          titulo="Excluir em lote"
          mensagem={`Excluir ${selecionados.size} ${selecionados.size === 1 ? 'artigo' : 'artigos'}? Esta ação não pode ser desfeita.`}
          confirmarLabel={`Excluir ${selecionados.size}`}
          carregando={loteExcluindo}
          onConfirmar={() => void confirmarLote()}
          onCancelar={() => setLoteConfirmando(false)}
        />
      )}
    </div>
  )
}
