import axios from 'axios'

// --- Types ---

export interface User {
  id: string
  email: string
  phone: string
  nickname: string
  avatar: string
  created_at: string
  updated_at: string
}

export interface AuthResponse {
  token: string
  refresh_token: string
  expires_at: number
  user: User
}

export interface ApiResponse<T = unknown> {
  code: number
  message: string
  data: T
}

// --- Axios instance ---

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
  headers: {
    'Content-Type': 'application/json',
  },
})

// Request interceptor: attach auth token
http.interceptors.request.use((config) => {
  const token = localStorage.getItem('anbanwriter_token')
  if (token) {
    config.headers.Authorization = `Bearer ${token}`
  }
  return config
})

// Response interceptor: handle 401
http.interceptors.response.use(
  (response) => response,
  (error) => {
    if (error.response?.status === 401) {
      localStorage.removeItem('anbanwriter_token')
      localStorage.removeItem('anbanwriter_refresh_token')
      localStorage.removeItem('anbanwriter_user')
      window.dispatchEvent(new CustomEvent('auth:token-expired'))
    }
    return Promise.reject(error)
  },
)

// --- Helper to extract data from ApiResponse ---

async function unwrap<T>(request: Promise<{ data: ApiResponse<T> }>): Promise<T> {
  const response = await request
  return response.data.data
}

// --- API ---

export const api = {
  // Auth
  auth: {
    register: (email: string, password: string, nickname?: string) =>
      unwrap<AuthResponse>(http.post('/auth/register', { email, password, nickname })),

    login: (email: string, password: string) =>
      unwrap<AuthResponse>(http.post('/auth/login', { email, password })),

    refresh: (refreshToken: string) =>
      unwrap<AuthResponse>(http.post('/auth/refresh', { refresh_token: refreshToken })),

    logout: () =>
      unwrap<void>(http.post('/auth/logout')),

    me: () =>
      unwrap<User>(http.get('/auth/me')),

    wxLogin: (code: string, nickname?: string, avatar?: string) =>
      unwrap<AuthResponse>(http.post('/auth/wx-login', { code, nickname, avatar })),
  },

  // Placeholder for future API groups
  tasks: {
    list: () =>
      unwrap<unknown[]>(http.get('/tasks')),
    get: (id: string) =>
      unwrap<unknown>(http.get(`/tasks/${id}`)),
    create: (data: unknown) =>
      unwrap<unknown>(http.post('/tasks', data)),
  },

  plans: {
    list: () =>
      unwrap<unknown[]>(http.get('/plans')),
    get: (id: string) =>
      unwrap<unknown>(http.get(`/plans/${id}`)),
  },

  settings: {
    get: () =>
      unwrap<unknown>(http.get('/settings')),
    update: (data: unknown) =>
      unwrap<unknown>(http.put('/settings', data)),
  },
}

export default http
