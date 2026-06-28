import { describe, it, expect, vi, beforeEach } from 'vitest'

import { commandPaletteStore, commandPaletteAccelerator } from './command-palette'

describe('commandPaletteStore', () => {
  beforeEach(() => {
    // The store is a module singleton; reset to a known closed state per test.
    commandPaletteStore.close()
  })

  it('starts closed and notifies subscribers on open/close', () => {
    const listener = vi.fn()
    commandPaletteStore.subscribe(listener)

    expect(commandPaletteStore.getSnapshot()).toBe(false)

    commandPaletteStore.open()
    expect(commandPaletteStore.getSnapshot()).toBe(true)
    expect(listener).toHaveBeenCalledTimes(1)

    commandPaletteStore.close()
    expect(commandPaletteStore.getSnapshot()).toBe(false)
    expect(listener).toHaveBeenCalledTimes(2)
  })

  it('open() is idempotent — no duplicate emit when already open', () => {
    const listener = vi.fn()
    commandPaletteStore.subscribe(listener)

    commandPaletteStore.open()
    commandPaletteStore.open()
    commandPaletteStore.open()

    expect(commandPaletteStore.getSnapshot()).toBe(true)
    expect(listener).toHaveBeenCalledTimes(1)
  })

  it('toggle() flips state and emits exactly once per flip', () => {
    const listener = vi.fn()
    commandPaletteStore.subscribe(listener)

    commandPaletteStore.toggle()
    expect(commandPaletteStore.getSnapshot()).toBe(true)
    commandPaletteStore.toggle()
    expect(commandPaletteStore.getSnapshot()).toBe(false)

    expect(listener).toHaveBeenCalledTimes(2)
  })

  it('unsubscribe stops emitting to that listener', () => {
    const listener = vi.fn()
    const unsub = commandPaletteStore.subscribe(listener)

    commandPaletteStore.open()
    expect(listener).toHaveBeenCalledTimes(1)

    unsub()
    commandPaletteStore.toggle()
    commandPaletteStore.toggle()

    expect(listener).toHaveBeenCalledTimes(1)
  })

  it('exposes a platform accelerator string', () => {
    // jsdom reports a non-Apple UA → Ctrl K; either way it must be a non-empty label.
    expect(typeof commandPaletteAccelerator).toBe('string')
    expect(commandPaletteAccelerator.length).toBeGreaterThan(0)
  })
})
