import { createContext, useContext, useEffect, useState, useCallback, useRef, type ReactNode } from 'react'
import { useQueryClient } from '@tanstack/react-query'
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
  const queryClient = useQueryClient()
  // Track the signed-in user so we drop cached queries only when the identity
  // actually changes (login / account switch) — not on same-user token refresh,
  // which would otherwise trigger a wasteful full refetch.
  const prevUserIdRef = useRef<string | null>(state.user?.id ?? null)

  // Drop all cached queries (templates/channels/plans carry the previous user's
  // image URLs) and reset the identity tracker. Used on logout / token expiry.
  const clearUserCache = useCallback(() => {
    prevUserIdRef.current = null
    queryClient.clear()
  }, [queryClient])

  // Listen for token expiration events dispatched by the API interceptor
  useEffect(() => {
    const handler = () => {
      clearUserCache()
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
    }
    window.addEventListener('auth:token-expired', handler)
    return () => window.removeEventListener('auth:token-expired', handler)
  }, [clearUserCache])

  const login = useCallback((token: string, refreshToken: string, user: User) => {
    // Clear cached queries when the signed-in identity changes (initial login or
    // account switch) so we never render another account's user-scoped data.
    // Same-user token refresh leaves the cache intact.
    if (prevUserIdRef.current !== user.id) {
      queryClient.clear()
    }
    prevUserIdRef.current = user.id
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(REFRESH_TOKEN_KEY, refreshToken)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    setState({ token, refreshToken, user, isAuthenticated: true })
  }, [queryClient])

  const logout = useCallback(async () => {
    try {
      await api.auth.logout()
    } catch {
      // Ignore logout API errors
    }
    clearStoredState()
    clearUserCache()
    setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
  }, [clearUserCache])

  const refreshAuthToken = useCallback(async () => {
    const storedRefresh = localStorage.getItem(REFRESH_TOKEN_KEY)
    if (!storedRefresh) {
      clearStoredState()
      clearUserCache()
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
      return
    }

    try {
      const response: AuthResponse = await api.auth.refresh(storedRefresh)
      login(response.token, response.refresh_token, { ...response.user, has_password: response.has_password, max_invites: response.max_invites })
    } catch {
      clearStoredState()
      clearUserCache()
      setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
    }
  }, [login, clearUserCache])

  const setUser = useCallback((user: User) => {
    localStorage.setItem(USER_KEY, JSON.stringify(user))
    setState((prev) => ({ ...prev, user, isAuthenticated: prev.isAuthenticated }))
  }, [])

  // Verify token on mount by fetching current user
  useEffect(() => {
    if (state.token && !state.user) {
      api.auth.me().then((user) => {
        setUser(user)
      }).catch(() => {
        clearStoredState()
        setState({ token: null, refreshToken: null, user: null, isAuthenticated: false })
      })
    }
  }, [state.token, state.user, setUser])

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
