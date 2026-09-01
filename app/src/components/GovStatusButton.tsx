// PROVISORIO — Botão debug governança. Remover depois. Mostra JSON de GET /api/governanca/status com botão Copiar.
// Fácil de remover: deletar este arquivo e remover import/uso em App.tsx
import { useState } from 'react'
import { api } from '../api'

type StatusData = Record<string, unknown> | null

export function GovStatusButton() {
  const [open, setOpen] = useState(false)
  const [data, setData] = useState<StatusData>(null)
  const [loading, setLoading] = useState(false)
  const [erro, setErro] = useState<string | null>(null)
  const [copiado, setCopiado] = useState(false)

  const buscar = async () => {
    setLoading(true)
    setErro(null)
    setData(null)
    setOpen(true)
    try {
      // garante X-Device-Id presente (session cria e persiste em localStorage ana_device_id)
      try {
        const { getOrCreateDeviceId } = await import('../offline/session')
        await getOrCreateDeviceId()
      } catch {
        // ignora, request ainda tenta
      }
      const res = await api.getGovernancaStatus()
      // api.getGovernancaStatus já retorna JSON parseado com {ip, deviceId, autorizado, ...}
      setData(res as unknown as Record<string, unknown>)
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      // tenta incluir status se for ApiError
      const status = (e as { status?: number })?.status
      setErro(status ? `Erro ${status}: ${msg}` : msg)
    } finally {
      setLoading(false)
    }
  }

  const copiar = async () => {
    if (!data) return
    const texto = JSON.stringify(data, null, 2)
    try {
      if (navigator.clipboard?.writeText) {
        await navigator.clipboard.writeText(texto)
        setCopiado(true)
        window.setTimeout(() => setCopiado(false), 2000)
        return
      }
      throw new Error('clipboard indisponível')
    } catch {
      // fallback execCommand (iPad older webview)
      try {
        const ta = document.createElement('textarea')
        ta.value = texto
        ta.setAttribute('readonly', '')
        ta.style.position = 'absolute'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.select()
        const ok = document.execCommand('copy')
        document.body.removeChild(ta)
        if (ok) {
          setCopiado(true)
          window.setTimeout(() => setCopiado(false), 2000)
        } else {
          setErro('Falha ao copiar — copie manualmente')
        }
      } catch {
        setErro('Falha ao copiar — copie manualmente')
      }
    }
  }

  const fechar = () => {
    setOpen(false)
    setCopiado(false)
  }

  return (
    <>
      <button type="button" className="btn btn-sm" onClick={() => void buscar()} aria-label="Ver meu status de governança">
        Meu status
      </button>
      {open && (
        <div className="dialog-overlay" role="dialog" aria-modal="true" onClick={fechar}>
          <div className="dialog" style={{ maxWidth: 520 }} onClick={(e) => e.stopPropagation()}>
            <h2 className="dialog-titulo">Meu status</h2>
            <p className="dialog-mensagem" style={{ fontSize: 'var(--fs-sm)' }}>
              GET /api/governanca/status — ip, deviceId, autorizado (provisório)
            </p>

            {loading && <p className="dialog-mensagem">Carregando…</p>}
            {erro && (
              <div className="home-erro" role="alert" style={{ textAlign: 'left', wordBreak: 'break-word' }}>
                {erro}
              </div>
            )}
            {data && (
              <pre
                style={{
                  background: 'var(--surface-2)',
                  border: '1px solid var(--border)',
                  borderRadius: 'var(--r-s)',
                  padding: '12px',
                  fontFamily: 'var(--font-mono)',
                  fontSize: 12,
                  lineHeight: 1.5,
                  maxHeight: '50vh',
                  overflow: 'auto',
                  whiteSpace: 'pre-wrap',
                  wordBreak: 'break-word',
                  margin: 0,
                }}
              >
                {JSON.stringify(data, null, 2)}
              </pre>
            )}

            <div className="dialog-acoes">
              <button type="button" className="btn" onClick={fechar}>
                Fechar
              </button>
              <button type="button" className="btn btn-primary" onClick={() => void copiar()} disabled={!data}>
                {copiado ? 'Copiado!' : 'Copiar'}
              </button>
            </div>
          </div>
        </div>
      )}
    </>
  )
}
