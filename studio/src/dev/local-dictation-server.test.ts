import { describe, expect, it } from 'vitest'
import { acceptsLocalVoice } from '../../scripts/local-dictation'

function request(method = 'POST', headers = {}, remoteAddress = '127.0.0.1') {
  return { method, headers: { host: '127.0.0.1:5174', origin: 'http://127.0.0.1:5174', 'x-anban-local-voice': '1', ...headers }, socket: { remoteAddress } }
}

describe('local preview voice access boundary', () => {
  it('accepts only same-origin loopback requests with the explicit header', () => {
    expect(acceptsLocalVoice(request())).toBe(true)
    expect(acceptsLocalVoice(request('GET'))).toBe(true)
    expect(acceptsLocalVoice(request('POST', { origin: 'https://external.example' }))).toBe(false)
    expect(acceptsLocalVoice(request('POST', { 'x-anban-local-voice': undefined }))).toBe(false)
    expect(acceptsLocalVoice(request('POST', { host: 'external.example' }))).toBe(false)
    expect(acceptsLocalVoice(request('POST', {}, '192.168.1.1'))).toBe(false)
    expect(acceptsLocalVoice(request('OPTIONS'))).toBe(false)
  })
})
