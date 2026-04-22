import { createContext, useContext, useEffect, useState, useCallback, type ReactNode } from 'react'
import { api } from '@/lib/api'
import type { User, AuthResponse } from '@/types'

// --- Types ---

interface AuthState {
  token: string | null
  refreshToken: string | null
  user: User | null
  isAuthenticated: boolean
}

interface AuthContextValue extends AuthState {
  login: (token: string, refreshToken: string, user: User) => void
  logout: () => Promise<void>
  refreshAuthToken: () => Promise<void>
  setUser: (user: User) => void
}

const TOKEN_KEY = 'anbanwriter_token'
const REFRESH_TOKEN_KEY = 'anbanwriter_refresh_token'
const USER_KEY = 'anbanwriter_user'

// --- Context ---

const AuthContext = createContext<AuthContextValue | null>(null)

// --- Helpers ---

function loadStoredState(): AuthState {
  const token = localStorage.getItem(TOKEN_KEY)
  const refreshToken = localStorage.getItem(REFRESH_TOKEN_KEY)
  const userRaw = localStorage.getItem(USER_KEY)

  let user: User | null = null
  if (userRaw) {
    try {
      user = JSON.parse(userRaw) as User
    } catch {
      localStorage.removeItem(USER_KEY)
    }
  }

  return {
    token,
    refreshToken,
    user,
    isAuthenticated: !!token && !!user,
  }
}

function clearStoredState() {
  localStorage.removeItem(TOKEN_KEY)
  localStorage.removeItem(REFRESH_TOKEN_KEY)
  localStorage.removeItem(USER_KEY)
}

// --- Provider ---

export function AuthProvider({ children }: { children: ReactNode }) {
  const [state, setState] = useState<AuthState>(loadStoredState)

  // Listen for token expiration events dispatched by the API interceptor
  useEffect(() => {
    const handler = () => {
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
    }
    window.addEventListener('auth:token-expired', handler)
    return () => window.removeEventListener('auth:token-expired', handler)
  }, [])

  const login = useCallback((token: string, refreshToken: string, user: User) => {
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    setState({ token, refreshToken, user, isAuthenticated: true })
  }, [])

  const logout = useCallback(async () => {
    try {
      await api.auth.logout()
    } catch {
      // Ignore logout API errors
    }
    clearStoredState()
    setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
  }, [])

  const refreshAuthToken = useCallback(async () => {
    const storedRefresh = localStorage.getItem(REFRESH_TOKEN_KEY)
    if (!storedRefresh) {
      clearStoredState()
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
      return
    }

    try {
      const response: AuthResponse = await api.auth.refresh(storedRefresh)
      login(response.token, response.refresh_token, response.user)
    } catch {
      clearStoredState()
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
    }
  }, [login])

  const setUser = useCallback((user: User) => {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    setState((prev) => ({ ...prev, user, isAuthenticated: prev.isAuthenticated }))
  }, [])

  // Fetch current user on mount to ensure data is fresh
  useEffect(() => {
    if (state.token) {
      api.auth.me().then((user) => {
        setUser(user)
      }).catch(() => {
        clearStoredState()
        setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
      })
    }
  }, [state.token, setUser])

  return (
    <AuthContext.Provider value={{ ...state, login, logout, refreshAuthToken, setUser }}>
      {children}
    </AuthContext.Provider>
  )
}

// --- Hook ---

export function useAuth(): AuthContextValue {
  const context = useContext(AuthContext)
  if (!context) {
    throw new Error('useAuth must be used within an AuthProvider')
  }
  return context
}
