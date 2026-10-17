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

  it('resumes from a persisted event id and reports newly received ids', async () => {
    const onEvent = vi.fn()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(response('event:session.status\nid:8\ndata:{"status":"running"}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token', undefined, { lastEventId: '7', onEvent })

    await expect(stream.next()).resolves.toMatchObject({ value: { event: 'session.status', id: '8' }, done: false })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/interactive/sessions/session-1/events', expect.objectContaining({
      headers: expect.objectContaining({ 'Last-Event-ID': '7' }),
    }))
    expect(onEvent).toHaveBeenCalledWith(expect.objectContaining({ id: '8' }))
  })

  it('reports an idle SSE connection as ready before its first event', async () => {
    const onConnected = vi.fn()
    let enqueue: ((value: Uint8Array) => void) | undefined
    const body = new ReadableStream<Uint8Array>({
      start(controller) {
        enqueue = (value) => controller.enqueue(value)
      },
    })
    vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(body, { headers: { 'Content-Type': 'text/event-stream' } }))
    const stream = streamInteractiveSession('session-1', 'token', undefined, { onConnected })
    const nextEvent = stream.next()

    await vi.waitFor(() => expect(onConnected).toHaveBeenCalledTimes(1))
    enqueue?.(new TextEncoder().encode('event:session.ready\ndata:{}\n\n'))
    await expect(nextEvent).resolves.toMatchObject({ value: { event: 'session.ready' }, done: false })
    await stream.return(undefined)
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

  it('keeps retrying transient network failures until the session can be resumed', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch')
      .mockRejectedValueOnce(new Error('network error'))
      .mockRejectedValueOnce(new Error('network error'))
      .mockRejectedValueOnce(new Error('network error'))
      .mockRejectedValueOnce(new Error('network error'))
      .mockResolvedValueOnce(response('event:session.status\nid:1\ndata:{"status":"running"}\n\n'))
    const stream = streamInteractiveSession('session-1', 'token')
    const nextEvent = stream.next()

    await vi.advanceTimersByTimeAsync(10_000)
    await expect(nextEvent).resolves.toMatchObject({ value: { event: 'session.status', id: '1' }, done: false })
    expect(fetchMock).toHaveBeenCalledTimes(5)
  })

  it('fails closed after the reconnect budget is exhausted', async () => {
    vi.useFakeTimers()
    const fetchMock = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('network error'))
    const controller = new AbortController()
    const stream = streamInteractiveSession('session-1', 'token', controller.signal)
    const nextEvent = stream.next()
    let settled = false
    void nextEvent.then(() => { settled = true }, () => { settled = true })

    await vi.advanceTimersByTimeAsync(40_000)
    expect(settled).toBe(true)
    expect(fetchMock).toHaveBeenCalledTimes(7)
    controller.abort()
    await nextEvent.catch(() => undefined)
  })
})
