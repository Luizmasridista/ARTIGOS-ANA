import { lazy, Suspense, useState } from 'react'
import { useAuth } from '../context/AuthContext'
import { ApiError } from '../api'

const GrokBanner = lazy(() => import('./GrokBanner').then((m) => ({ default: m.GrokBanner })))

export function formatRetryAfter(seg: number): string {
  if (seg <= 60) return `${seg} segundos`
  const min = Math.ceil(seg / 60)
  return `${min} minuto${min > 1 ? 's' : ''}`
}

export function mapLoginError(err: unknown): { mensagem: string; retryAfter?: number } {
  if (err instanceof ApiError) {
    if (err.status === 423) {
      const ra = err.retryAfter ?? 900
      return { mensagem: `muitas tentativas, tente novamente em ${formatRetryAfter(ra)}`, retryAfter: ra }
    }
    if (err.status === 401) return { mensagem: err.message }
    if (err.status === 400) return { mensagem: 'selecione um usuário' }
    return { mensagem: err.message, retryAfter: err.retryAfter }
  }
  if (err instanceof Error) return { mensagem: err.message }
  return { mensagem: 'Falha ao entrar' }
}

export function validarLoginInput(nome: string, senha: string): string | null {
  const t = nome.trim()
  if (!t) return 'selecione um usuário'
  if (t !== 'Ana Bagatinii' && t !== 'Luiz') return 'usuário inválido'
  if (!senha) return 'digite a senha'
  return null
}

export function Home() {
  const { login } = useAuth()
  const [carregando, setCarregando] = useState<string | null>(null)
  const [erro, setErro] = useState<string | null>(null)
  const [retryAfter, setRetryAfter] = useState<number | null>(null)
  const [senha, setSenha] = useState('')

  const handleLogin = async (nome: string) => {
    console.log('[Home] clique login', { nome, timestamp: new Date().toISOString() })
    setErro(null)
    setRetryAfter(null)
    const err = validarLoginInput(nome, senha)
    if (err) {
      console.warn('[Home] validar falhou', { nome, err })
      setErro(err)
      return
    }
    setCarregando(nome)
    try {
      console.log('[Home] enviando POST /api/auth/login', { nome })
      await login(nome, senha)
      console.log('[Home] login sucesso', { nome })
    } catch (err) {
      console.error('[Home] login erro', { nome, err, status: err instanceof ApiError ? err.status : undefined, message: err instanceof Error ? err.message : String(err) })
      if (err instanceof ApiError) {
        console.error('[Home] ApiError', { status: err.status, retryAfter: err.retryAfter, message: err.message })
        if (err.status === 423) {
          const ra = err.retryAfter ?? 900
          setRetryAfter(ra)
          setErro(`muitas tentativas, tente novamente em ${formatRetryAfter(ra)}`)
        } else if (err.status === 400) {
          setErro('selecione um usuário')
        } else {
          // 401 (usuário/senha) e 403 (conta sem senha fora do PC): texto do servidor
          setErro(err.message)
        }
      } else if (err instanceof Error) {
        setErro(err.message)
      } else {
        setErro('Falha ao entrar')
      }
    } finally {
      setCarregando(null)
      console.log('[Home] login finalizado', { nome })
    }
  }

  return (
    <div className="home">
      <div style={{ width: '100%', maxWidth: 380, display: 'flex', flexDirection: 'column', gap: 16 }}>
        <Suspense fallback={null}>
          <GrokBanner />
        </Suspense>
        <div className="home-card" role="main" aria-labelledby="home-titulo">
          <h1 id="home-titulo" className="home-titulo">
            Artigos Ana
          </h1>
          <p className="home-subtitulo">Escolha o usuário para entrar</p>

          <div className="home-senha">
            <input
              className="input"
              type="password"
              placeholder="Senha"
              value={senha}
              onChange={(e) => setSenha(e.target.value)}
              aria-label="Senha"
              autoComplete="current-password"
              disabled={carregando !== null}
            />
          </div>

          <div className="home-usuarios" role="group" aria-label="Escolha o usuário">
            <button
              type="button"
              className="btn btn-primary home-botao home-botao-usuario"
              onClick={() => void handleLogin('Ana Bagatinii')}
              disabled={carregando !== null}
              aria-label="Entrar como Ana Bagatinii"
            >
              {carregando === 'Ana Bagatinii' ? 'Entrando…' : 'Entrar como Ana Bagatinii'}
            </button>
            <button
              type="button"
              className="btn home-botao home-botao-usuario"
              onClick={() => void handleLogin('Luiz')}
              disabled={carregando !== null}
              aria-label="Entrar como Luiz"
            >
              {carregando === 'Luiz' ? 'Entrando…' : 'Entrar como Luiz'}
            </button>
          </div>

          {erro && (
            <div className="home-erro" role="alert" aria-live="assertive">
              {erro}
              {retryAfter !== null && retryAfter > 0 && (
                <span className="home-retry"> ({retryAfter}s)</span>
              )}
            </div>
          )}
        </div>
      </div>
    </div>
  )
}
