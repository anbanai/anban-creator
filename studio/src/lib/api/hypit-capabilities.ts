import { http, unwrap } from '@/lib/http-client'
import type { HypitCapabilities } from '@/types'
export const hypitCapabilitiesApi = { list: (sourceTaskId?: string) => unwrap<HypitCapabilities>(http.get('/hypit-capabilities', sourceTaskId ? { params: { source_task_id: sourceTaskId } } : undefined)) }
