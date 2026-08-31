import { useEffect, useRef, useState } from 'react'
import { IconeNota } from './Icones'

const CORES_NOTA = [
  { nome: 'Amarelo', valor: '#FFEB3B' },
  { nome: 'Laranja', valor: '#FFE03B' },
  { nome: 'Verde', valor: '#9EE6A8' },
  { nome: 'Azul', valor: '#8FC1FF' },
  { nome: 'Rosa', valor: '#FFB3D1' },
]

const LS_MINIMIZADA = 'ilha-nota:minimizada'

function lerMinimizada(): boolean {
  try {
    return localStorage.getItem(LS_MINIMIZADA) === '1'
  } catch {
    return false
  }
}

interface Props {
  numPaginas: number
  prefill: { texto: string; pagina: number; marcacaoId?: number } | null
  prefillNonce: number
  onCriarNota: (pagina: number, texto: string, marcacaoId?: number, tags?: string[], cor?: string) => Promise<boolean>
}

export function IlhaNota({ numPaginas, prefill, prefillNonce, onCriarNota }: Props) {
  const [paginaNota, setPaginaNota] = useState(1)
  const [textoNota, setTextoNota] = useState('')
  const [marcacaoIdNota, setMarcacaoIdNota] = useState<number | undefined>(undefined)
  const [tagsComposer, setTagsComposer] = useState<string[]>([])
  const [tagInput, setTagInput] = useState('')
  const [corNota, setCorNota] = useState<string>('#FFEB3B')
  const [salvando, setSalvando] = useState(false)
  const [minimizada, setMinimizada] = useState<boolean>(() => {
    if (typeof window === 'undefined') return false
    return lerMinimizada()
  })
  const textareaRef = useRef<HTMLTextAreaElement>(null)

  useEffect(() => {
    try {
      localStorage.setItem(LS_MINIMIZADA, minimizada ? '1' : '0')
    } catch {
      // ignore
    }
  }, [minimizada])

  // sync fallback class on .leitor for browsers without :has() and for padding
  useEffect(() => {
    const el = document.querySelector('.leitor')
    if (!el) return
    el.classList.toggle('leitor--ilha-minimizada', minimizada)
    el.classList.toggle('leitor--ilha-expandida', !minimizada)
  }, [minimizada])

  useEffect(() => {
    if (prefillNonce === 0) return
    setPaginaNota(prefill?.pagina ?? 1)
    setTextoNota(prefill?.texto ?? '')
    setMarcacaoIdNota(prefill?.marcacaoId)
    // auto-expand when a selection is sent to the island
    setMinimizada(false)
    // focus with micro-delay to ensure mount/expand
    requestAnimationFrame(() => textareaRef.current?.focus())
  }, [prefillNonce, prefill])

  // when expanding via pill, focus textarea
  useEffect(() => {
    if (!minimizada) {
      requestAnimationFrame(() => textareaRef.current?.focus())
    }
  }, [minimizada])

  const adicionarTag = () => {
    const v = tagInput.trim()
    if (!v) return
    if (v.includes(',') || v.includes(';')) return
    if (v.length > 50) return
    if (tagsComposer.includes(v)) {
      setTagInput('')
      return
    }
    setTagsComposer((prev) => [...prev, v])
    setTagInput('')
  }

  const removerTagComposer = (tag: string) => {
    setTagsComposer((prev) => prev.filter((t) => t !== tag))
  }

  const salvarNota = async () => {
    const texto = textoNota.trim()
    if (!texto || salvando) return
    setSalvando(true)
    try {
      const ok = await onCriarNota(paginaNota, texto, marcacaoIdNota, tagsComposer, corNota)
      if (ok) {
        setTextoNota('')
        setMarcacaoIdNota(undefined)
        setTagsComposer([])
        setTagInput('')
        setCorNota('#FFEB3B')
        // keep focus for next note
        requestAnimationFrame(() => textareaRef.current?.focus())
      }
    } finally {
      setSalvando(false)
    }
  }

  // auto-resize textarea — compact hierarchy: min 72, max 120 (8px grid)
  const ajustarAltura = () => {
    const el = textareaRef.current
    if (!el) return
    el.style.height = 'auto'
    const max = 120
    const h = Math.min(el.scrollHeight, max)
    el.style.height = `${Math.max(72, h)}px`
    el.style.overflowY = el.scrollHeight > max ? 'auto' : 'hidden'
  }

  useEffect(() => {
    if (!minimizada) ajustarAltura()
  }, [textoNota, minimizada])

  if (minimizada) {
    return (
      <div
        className="ilha-nota ilha-nota--minimizada"
        role="region"
        aria-label="Nova nota (minimizada)"
      >
        <button
          type="button"
          className="ilha-nota-pill"
          onClick={() => setMinimizada(false)}
          aria-label="Expandir ilha de nota — Nova nota"
          title="Nova nota"
        >
          <span className="ilha-nota-pill-icone" aria-hidden="true">
            <IconeNota size={16} />
          </span>
          <span className="ilha-nota-pill-texto">Nova nota</span>
          <span className="ilha-nota-pill-plus" aria-hidden="true">+</span>
        </button>
      </div>
    )
  }

  return (
    <div className="ilha-nota ilha-nota--expandida" role="region" aria-label="Nova nota">
      {/* Header: title 14/650 vs hint 11/muted — distinct hierarchy, gap 12 */}
      <div className="ilha-nota-cabecalho">
        <span className="ilha-nota-cabecalho-titulo">Nova nota</span>
        <span className="ilha-nota-cabecalho-hint" aria-hidden="true">
          Ctrl+Enter para salvar
        </span>
        <button
          type="button"
          className="ilha-nota-minimizar"
          onClick={() => setMinimizada(true)}
          aria-label="Minimizar ilha de nota"
          title="Minimizar"
        >
          <svg
            width="14"
            height="14"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth="2"
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
          >
            <path d="M6 9l6 6 6-6" />
          </svg>
        </button>
      </div>

      {/* Toolbar pill: Página + Cor grouped, var(--surface-2), compact */}
      <div className="ilha-nota-topo">
        <div className="ilha-nota-ferramentas">
          <label htmlFor="ilha-pagina" className="ilha-nota-label">
            Página
          </label>
          <select
            id="ilha-pagina"
            className="input ilha-nota-select"
            value={paginaNota}
            onChange={(e) => setPaginaNota(Number(e.target.value))}
            aria-label="Página da nota"
          >
            {Array.from({ length: Math.max(numPaginas, 1) }, (_, i) => i + 1).map((n) => (
              <option key={n} value={n}>
                {n}
              </option>
            ))}
          </select>
          <span className="ilha-nota-separador" aria-hidden="true" />
          <span className="ilha-nota-label ilha-nota-label--cor">Cor</span>
          <div className="ilha-nota-cores" role="group" aria-label="Cor da nota">
            {CORES_NOTA.map((c) => (
              <button
                key={c.valor}
                type="button"
                className={`ilha-nota-cor ${corNota === c.valor ? 'ativa' : ''}`}
                style={{ background: c.valor }}
                title={c.nome}
                aria-label={`Cor ${c.nome}`}
                aria-pressed={corNota === c.valor}
                onClick={() => setCorNota(c.valor)}
              />
            ))}
          </div>
        </div>
        {tagsComposer.length > 0 && (
          <div className="ilha-nota-chips" aria-label="Tags selecionadas">
            {tagsComposer.map((tag) => (
              <span key={tag} className="chip-tag ativa ilha-chip">
                {tag}
                <button
                  type="button"
                  className="chip-tag-remove"
                  onClick={() => removerTagComposer(tag)}
                  aria-label={`Remover tag ${tag}`}
                >
                  ×
                </button>
              </span>
            ))}
          </div>
        )}
      </div>

      <textarea
        ref={textareaRef}
        className="input ilha-nota-textarea"
        placeholder="Escreva sua nota…"
        value={textoNota}
        onChange={(e) => setTextoNota(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault()
            void salvarNota()
          }
          if (e.key === 'Escape') {
            ;(e.target as HTMLTextAreaElement).blur()
          }
        }}
        aria-label="Texto da nova nota"
        rows={1}
      />

      {/* Footer: tag flex1 36 + Adicionar ghost + Salvar primary 36, gap 8 */}
      <div className="ilha-nota-rodape">
        <div className="ilha-nota-tag-entrada">
          <input
            className="input ilha-nota-tag-input"
            placeholder="Tag + Enter"
            value={tagInput}
            onChange={(e) => setTagInput(e.target.value)}
            onKeyDown={(e) => {
              if (e.key === 'Enter' || e.key === ',') {
                e.preventDefault()
                adicionarTag()
              }
              if (e.key === 'Backspace' && tagInput === '' && tagsComposer.length > 0) {
                setTagsComposer((prev) => prev.slice(0, -1))
              }
            }}
            aria-label="Adicionar tag"
          />
          <button
            type="button"
            className="btn btn-ghost btn-sm ilha-nota-adicionar"
            onClick={adicionarTag}
            disabled={!tagInput.trim()}
            aria-label="Adicionar tag"
          >
            Adicionar
          </button>
        </div>
        <button
          type="button"
          className="btn btn-primary ilha-nota-salvar"
          onClick={() => void salvarNota()}
          disabled={salvando || textoNota.trim() === ''}
          aria-label="Salvar nota"
        >
          Salvar
        </button>
      </div>
    </div>
  )
}
