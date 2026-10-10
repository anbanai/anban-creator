import { afterEach, describe, expect, it, vi } from 'vitest'
import { streamInteractiveSession } from './interactive-events'

function response(body: string) {
  return new Response(body, { headers: { 'Content-Type': 'text/event-stream' } })
}

describe('streamInteractiveSession', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('starts without a Last-Event-ID header', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response('event:session.closed\nid:7\ndata:{}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token')

    await expect(stream.next()).resolves.toMatchObject({ value: { event: 'session.closed', id: '7' }, done: false })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/interactive/sessions/session-1/events', expect.objectContaining({
      headers: {
        Authorization: 'Bearer token',
        Accept: 'text/event-stream',
      },
    }))
  })

  it('reconnects with the latest event id after a stream closes', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response('event:session.status\nid:8\ndata:{"status":"running"}\n\n'))
      .mockResolvedValueOnce(response('event:session.closed\nid:9\ndata:{}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token')

    await expect(stream.next()).resolves.toMatchObject({ value: { event: 'session.status', id: '8' }, done: false })
    const nextEvent = stream.next()
    await vi.advanceTimersByTimeAsync(250)
    await expect(nextEvent).resolves.toMatchObject({ value: { event: 'session.closed', id: '9' }, done: false })
    expect(fetchMock).toHaveBeenNthCalledWith(2, '/api/v1/interactive/sessions/session-1/events', expect.objectContaining({
      headers: {
        Authorization: 'Bearer token',
        Accept: 'text/event-stream',
        'Last-Event-ID': '8',
      },
    }))
  })

  it('stops after a terminal event without reconnecting', async () => {
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response('event:session.expired\ndata:{}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token')

    await expect(stream.next()).resolves.toMatchObject({ value: { event: 'session.expired' }, done: false })
    await expect(stream.next()).resolves.toEqual({ value: undefined, done: true })
    expect(fetchMock).toHaveBeenCalledTimes(1)
  })

  it('fails closed when the server reports that event history was compacted', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(null, { status: 409 }))
    const stream = streamInteractiveSession('session-1', 'token')

    await expect(stream.next()).rejects.toThrow('会话历史已失效，请重新开始创建')
  })

  it('fails closed when a reconnect starts after an unavailable history gap', async () => {
    vi.useFakeTimers()
    vi.spyOn(globalThis, 'fetch')
      .mockResolvedValueOnce(response('event:session.status\nid:7\ndata:{"status":"running"}\n\n'))
      .mockResolvedValueOnce(response('event:session.status\nid:9\ndata:{"status":"running"}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token')

    await expect(stream.next()).resolves.toMatchObject({ value: { id: '7' }, done: false })
    const nextEvent = stream.next()
    const nextEventAssertion = expect(nextEvent).rejects.toThrow('会话历史已失效，请重新开始创建')
    await vi.advanceTimersByTimeAsync(250)
    await nextEventAssertion
  })
})
