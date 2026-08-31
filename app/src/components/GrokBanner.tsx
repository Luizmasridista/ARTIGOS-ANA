import { useEffect, useState } from 'react'
import { IconeLink } from './Icones'

type GrokInfo = { url: string | null; status: string; error: string | null }

export function GrokBanner() {
  const [info, setInfo] = useState<GrokInfo>({ url: null, status: 'off', error: null })
  const [copiado, setCopiado] = useState(false)

  useEffect(() => {
    const api = window.artigosAna?.grok
    if (!api) return
    let cancel = false
    api.get().then((v) => {
      if (!cancel) setInfo(v)
    })
    const off = api.onUrl((url) => {
      if (!cancel) setInfo({ url, status: 'online', error: null })
    })
    const iv = window.setInterval(async () => {
      try {
        const v = await api.get()
        if (!cancel) setInfo(v)
      } catch {}
    }, 4000)
    return () => {
      cancel = true
      off()
      window.clearInterval(iv)
    }
  }, [])

  const api = window.artigosAna?.grok
  if (!api) return null

  const reiniciar = async () => {
    try {
      const v = await api.restart()
      setInfo(v)
    } catch {}
  }

  const copiar = async () => {
    if (!info.url) return
    try {
      await api.copiar(info.url)
      setCopiado(true)
      window.setTimeout(() => setCopiado(false), 2000)
    } catch {
      try {
        await navigator.clipboard.writeText(info.url)
        setCopiado(true)
        window.setTimeout(() => setCopiado(false), 2000)
      } catch {}
    }
  }

  const abrir = () => {
    if (info.url) void window.artigosAna?.abrirExterno(info.url)
  }

  const statusLabel =
    info.status === 'online' ? 'Ativo' : info.status === 'starting' ? 'Iniciando' : 'Offline'
  const statusClasse =
    info.status === 'online' ? 'online' : info.status === 'starting' ? 'starting' : 'offline'

  return (
    <div className="grok-banner" role="region" aria-label="Acesso web">
      <div className="grok-banner-header">
        <span className={`grok-status-dot ${statusClasse}`} aria-hidden="true" />
        <span className="grok-banner-titulo">Acesso web</span>
        <span className={`grok-status-badge ${statusClasse}`}>{statusLabel}</span>
      </div>

      {info.url ? (
        <>
          <div className="grok-banner-corpo">
            <div className="grok-url-wrap" title={info.url}>
              <IconeLink size={14} />
              <code className="grok-url">{info.url}</code>
            </div>
            <div className="grok-banner-acoes">
              <button type="button" className={`btn btn-sm ${copiado ? 'btn-primary' : ''}`} onClick={copiar}>
                {copiado ? 'Copiado!' : 'Copiar link'}
              </button>
              <button type="button" className="btn btn-sm" onClick={abrir}>
                Abrir
              </button>
            </div>
          </div>
          <span className="grok-banner-dica">Link seguro via Cloudflare — funciona em qualquer rede, mesmo em 4G</span>
        </>
      ) : (
        <div className="grok-banner-corpo grok-banner-corpo--vazio">
          <span className="grok-banner-mensagem">
            {info.status === 'starting'
              ? 'Gerando link seguro via Cloudflare…'
              : info.error
                ? `Erro: ${info.error}`
                : 'GROK inicia automático ao abrir o app'}
          </span>
          {info.status !== 'starting' && (
            <button type="button" className="btn btn-sm" onClick={reiniciar}>
              Reiniciar GROK
            </button>
          )}
        </div>
      )}
    </div>
  )
}
