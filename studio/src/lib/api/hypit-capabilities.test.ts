import { describe, expect, it } from 'vitest'
import { http, HttpResponse } from 'msw'
import { server } from '@/test/mocks/server'
import { hypitCapabilitiesApi } from './hypit-capabilities'

describe('replication capabilities API', () => {
  it('requests source capabilities only for a source task and leaves fresh requests global', async () => {
    const sources: (string | null)[] = []
    server.use(http.get('/api/v1/hypit-capabilities', ({ request }) => {
      sources.push(new URL(request.url).searchParams.get('source_task_id'))
      return HttpResponse.json({ code: 0, data: { enabled: true, configured: true, missing_configuration: [], limits: { max_duration_seconds: 180 } } })
    }))
    await hypitCapabilitiesApi.list('source-task')
    await hypitCapabilitiesApi.list()
    expect(sources).toEqual(['source-task', null])
  })
})
