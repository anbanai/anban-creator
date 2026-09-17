import { describe, it, expect } from 'vitest'
import { parseSSE } from './sse'

describe('parseSSE', () => {
  it('parses a single event', () => {
    const text = 'event:log\ndata:step 1\n\n'
    const events = parseSSE(text)
    expect(events).toHaveLength(1)
    expect(events[0]).toEqual({ event: 'log', data: 'step 1' })
  })

  it('parses multiple events', () => {
    const text = 'event:log\ndata:step 1\n\nevent:done\ndata:complete\n\n'
    const events = parseSSE(text)
    expect(events).toHaveLength(2)
    expect(events[0].event).toBe('log')
    expect(events[1].event).toBe('done')
  })

  it('handles multi-line data', () => {
    const text = 'event:log\ndata:line 1\ndata:line 2\n\n'
    const events = parseSSE(text)
    expect(events).toHaveLength(1)
    expect(events[0].data).toBe('line 1\nline 2')
  })

  it('handles event with id', () => {
    const text = 'event:lifecycle\ndata:test\nid:42\n\n'
    const events = parseSSE(text)
    expect(events[0].id).toBe('42')
  })

  it('returns empty for empty input', () => {
    expect(parseSSE('')).toHaveLength(0)
  })

  it('handles last event without trailing newline', () => {
    const text = 'event:log\ndata:test'
    const events = parseSSE(text)
    expect(events).toHaveLength(1)
    expect(events[0].data).toBe('test')
  })
})
