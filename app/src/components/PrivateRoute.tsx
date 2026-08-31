import type { ReactNode } from 'react'
import { useAuth } from '../context/AuthContext'

interface Props {
  children: ReactNode
  fallback?: ReactNode
}

export function isPrivateRouteAllowed(user: unknown, loading: boolean): boolean {
  if (loading) return false
  return !!user
}

export function PrivateRoute({ children, fallback }: Props) {
  const { user, loading } = useAuth()

  if (loading) {
    return (
      <div className="home-carregando" role="status" aria-live="polite">
        Carregando…
      </div>
    )
  }

  if (!user) {
    return fallback ? <>{fallback}</> : null
  }

  return <>{children}</>
}
