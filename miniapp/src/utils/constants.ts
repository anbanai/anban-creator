export const TOKEN_KEY = 'anbanwriter_token'
export const REFRESH_TOKEN_KEY = 'anbanwriter_refresh_token'
export const USER_KEY = 'anbanwriter_user'
export const API_BASE_URL = '/api/v1'

export const POLL_INTERVAL_RUNNING = 3000
export const AUTO_REFRESH_TOKEN_THRESHOLD = 5 * 60 * 1000 // 5 minutes before expiry

export const DEFAULT_PAGE_SIZE = 20
export const MAX_RETRY_COUNT = 3

export const IMAGE_RATIOS = [
  { value: '3:4', label: '3:4' },
  { value: '1:1', label: '1:1' },
  { value: '4:3', label: '4:3' },
  { value: '16:9', label: '16:9' },
] as const

export const TASK_QUANTITIES = [1, 2, 3, 4, 5] as const

export const SCHEDULE_PRESETS = [
  { label: '每天', cronExpr: '0 9 * * *' },
  { label: '每天 12:00', cronExpr: '0 12 * * *' },
  { label: '每天 18:00', cronExpr: '0 18 * * *' },
  { label: '周一至周五', cronExpr: '0 9 * * 1-5' },
  { label: '周一、三、五', cronExpr: '0 10 * * 1,3,5' },
  { label: '每周一', cronExpr: '0 9 * * 1' },
  { label: '每周六', cronExpr: '0 9 * * 6' },
] as const
