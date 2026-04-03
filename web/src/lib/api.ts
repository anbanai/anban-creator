import axios from 'axios'

// --- Core Types ---

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
  msg: string
  data: T
}

// --- Channel Types ---

export type ChannelPlatform = 'article' | 'xls' | 'rednote'
export type ChannelStatus = 'active' | 'archived'

export interface Channel {
  id: string
  user_id: string
  platform: ChannelPlatform
  name: string
  avatar_url: string
  description: string
  wechat_app_id: string
  keywords: string
  positioning: string
  style: string
  theme: string
  author: string
  image_api_config: string
  status: ChannelStatus
  created_at: string
  updated_at: string
}

export interface ChannelStats {
  total_tasks: number
  completed_tasks: number
  failed_tasks: number
  running_tasks: number
  pending_tasks: number
  success_rate: number
  last_activity_at: string
}

export interface ChannelDetail {
  channel: Channel
  stats: ChannelStats
}

export interface CreateChannelRequest {
  platform: string
  name: string
  wechat_app_id?: string
  wechat_secret?: string
  keywords?: string
  positioning?: string
  style?: string
  theme?: string
  author?: string
  image_api_config?: string
  description?: string
  avatar_url?: string
}

// --- Plan Types ---

export type PlanType = 'rednote' | 'article' | 'xls'
export type PlanStatus = 'active' | 'paused' | 'completed'

export interface Plan {
  id: string
  type: PlanType
  title: string
  description: string
  cron_expr: string
  topic_hint: string
  status: PlanStatus
  next_run_at: string
  channel_id: string
  created_at: string
  updated_at: string
}

export interface CreatePlanRequest {
  type: PlanType
  title: string
  description?: string
  cron_expr: string
  topic_hint?: string
  channel_id?: string
}

export interface UpdatePlanRequest {
  title?: string
  description?: string
  cron_expr?: string
  topic_hint?: string
}

// --- Task Types ---

export type TaskType = 'rednote' | 'article' | 'xls'
export type TaskStatus = 'pending' | 'running' | 'completed' | 'failed' | 'cancelled'

export interface Task {
  id: string
  type: TaskType
  topic: string
  status: TaskStatus
  progress: number
  error: string
  plan_id: number | null
  channel_id: string
  result: TaskResult
  created_at: string
  started_at: string
  completed_at: string
}

export interface TaskResult {
  files: TaskFile[] | null
  output: string
}

export interface TaskFile {
  id: string
  task_id: string
  role: string            // "image", "cover", "html", "markdown", "other"
  file_name: string
  mime_type: string
  file_size: number
  oss_url: string
  oss_key: string
  storage_provider: string // "oss" or "local"
  media_id?: string
  wechat_url?: string
  created_at: string
}

export interface CreateTaskRequest {
  type: TaskType
  topic: string
  channel_id?: string
}

// --- Timeline Types ---

export type TimelineItemType = 'task' | 'plan'

export interface TimelineItem {
  id: string
  type: TimelineItemType
  content_type: TaskType | PlanType
  title: string
  status: TaskStatus | PlanStatus
  scheduled_at: string
  completed_at: string
  plan_id?: number
  task_id?: string
  progress?: number
  error?: string
}

export interface TimelineResponse {
  items: TimelineItem[]
}

// --- Config Types ---

export type ConfigScope = 'article' | 'xls' | 'rednote'

export interface ImageApiConfig {
  provider: string
  model?: string
  base_url?: string
  api_key?: string
}

export interface UserConfig {
  scope: ConfigScope
  name: string
  keywords: string
  positioning: string
  style: string
  theme: string
  author: string
  wechat_app_id: string
  wechat_secret: string
  image_api_config: ImageApiConfig
}

export interface UpdateConfigRequest {
  name?: string
  keywords?: string
  positioning?: string
  style?: string
  theme?: string
  author?: string
  wechat_app_id?: string
  wechat_secret?: string
  image_api_config?: ImageApiConfig
}

// --- Paginated Response ---

export interface PaginatedResponse<T> {
  items: T[]
  total: number
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

  // Plans
  plans: {
    create: (data: CreatePlanRequest) =>
      unwrap<Plan>(http.post('/plans', data)),

    list: (params?: { offset?: number; limit?: number; channel_id?: string }) =>
      unwrap<PaginatedResponse<Plan>>(http.get('/plans', { params })),

    get: (id: string) =>
      unwrap<Plan>(http.get(`/plans/${id}`)),

    update: (id: string, data: UpdatePlanRequest) =>
      unwrap<Plan>(http.put(`/plans/${id}`, data)),

    delete: (id: string) =>
      unwrap<void>(http.delete(`/plans/${id}`)),

    pause: (id: string) =>
      unwrap<void>(http.post(`/plans/${id}/pause`)),

    resume: (id: string) =>
      unwrap<void>(http.post(`/plans/${id}/resume`)),
  },

  // Tasks
  tasks: {
    create: (data: CreateTaskRequest) =>
      unwrap<Task>(http.post('/tasks', data)),

    list: (params?: { offset?: number; limit?: number; status?: string; channel_id?: string }) =>
      unwrap<PaginatedResponse<Task>>(http.get('/tasks', { params })),

    get: (id: string) =>
      unwrap<Task>(http.get(`/tasks/${id}`)),

    cancel: (id: string) =>
      unwrap<void>(http.post(`/tasks/${id}/cancel`)),

    files: (id: string) =>
      unwrap<TaskFile[]>(http.get(`/tasks/${id}/files`)),

    streamUrl: (id: string) => `/api/v1/tasks/${id}/stream`,

    downloadFileBlob: async (taskId: string, fileId: string): Promise<Blob> => {
      const response = await http.get(`/tasks/${taskId}/files/${fileId}/download`, {
        responseType: 'blob',
      })
      return response.data
    },

    downloadZipBlob: async (taskId: string): Promise<Blob> => {
      const response = await http.get(`/tasks/${taskId}/files/zip`, {
        responseType: 'blob',
      })
      return response.data
    },

    fetchPreviewHTML: async (taskId: string): Promise<string> => {
      const response = await http.get(`/tasks/${taskId}/preview`, {
        responseType: 'text',
      })
      return response.data
    },
  },

  // Timeline
  timeline: {
    get: (from: string, to: string) =>
      unwrap<TimelineResponse>(http.get('/timeline', { params: { from, to } })),
  },

  // Channels
  channels: {
    list: (params?: { status?: string; platform?: string }) =>
      unwrap<Channel[]>(http.get('/channels', { params })),

    get: (id: string) =>
      unwrap<ChannelDetail>(http.get(`/channels/${id}`)),

    create: (data: CreateChannelRequest) =>
      unwrap<Channel>(http.post('/channels', data)),

    update: (id: string, data: Partial<CreateChannelRequest>) =>
      unwrap<Channel>(http.put(`/channels/${id}`, data)),

    archive: (id: string) =>
      unwrap<void>(http.patch(`/channels/${id}/archive`)),

    restore: (id: string) =>
      unwrap<void>(http.patch(`/channels/${id}/restore`)),

    delete: (id: string) =>
      unwrap<void>(http.delete(`/channels/${id}`)),
  },

  // Configs
  configs: {
    list: () =>
      unwrap<UserConfig[]>(http.get('/configs')),

    get: (scope: string) =>
      unwrap<UserConfig>(http.get(`/configs/${scope}`)),

    update: (scope: string, data: UpdateConfigRequest) =>
      unwrap<UserConfig>(http.put(`/configs/${scope}`, data)),
  },
}

export default http
