// Re-export the api object from the new modular structure
export { api } from './api/index'

// Re-export types for backward compatibility during migration
// Prefer importing from '@/types' directly in new code
export type * from '@/types'

// Re-export http client
export { default } from './http-client'
export { getApiErrorMessage } from './http-client'
