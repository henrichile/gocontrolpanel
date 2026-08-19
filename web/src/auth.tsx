import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react'
import type { ReactNode } from 'react'
import { api, tokens, type User } from './api'

interface AuthState {
  user: User | null
  loading: boolean
  login: (login: string, password: string) => Promise<void>
  logout: () => Promise<void>
  isAdmin: boolean
  isReseller: boolean
}

const AuthContext = createContext<AuthState | null>(null)

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  // Al montar, si hay token guardado recuperamos el perfil.
  useEffect(() => {
    let cancelled = false
    async function bootstrap() {
      if (!tokens.access) {
        setLoading(false)
        return
      }
      try {
        const me = await api.get<User>('/auth/me')
        if (!cancelled) setUser(me)
      } catch {
        tokens.clear()
      } finally {
        if (!cancelled) setLoading(false)
      }
    }
    void bootstrap()
    return () => {
      cancelled = true
    }
  }, [])

  // El cliente HTTP avisa cuando el refresh token deja de servir.
  useEffect(() => {
    const onExpired = () => setUser(null)
    window.addEventListener('gocp:session-expired', onExpired)
    return () => window.removeEventListener('gocp:session-expired', onExpired)
  }, [])

  const login = useCallback(async (loginValue: string, password: string) => {
    const res = await api.post<{ access_token: string; refresh_token: string; user: User }>(
      '/auth/login',
      { login: loginValue, password },
    )
    tokens.set(res.access_token, res.refresh_token)
    setUser(res.user)
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.post('/auth/logout')
    } catch {
      /* la sesión local se limpia igualmente */
    }
    tokens.clear()
    setUser(null)
  }, [])

  const value = useMemo<AuthState>(
    () => ({
      user,
      loading,
      login,
      logout,
      isAdmin: user?.role === 'admin',
      isReseller: user?.role === 'admin' || user?.role === 'reseller',
    }),
    [user, loading, login, logout],
  )

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext)
  if (!ctx) throw new Error('useAuth debe usarse dentro de <AuthProvider>')
  return ctx
}
