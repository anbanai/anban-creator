import { beforeEach, describe, expect, it } from 'vitest'
import { assistantMessageHandoffKey, clearAssistantMessageHandoff, readAssistantMessageHandoff, saveAssistantMessageHandoff } from './assistant-message-handoff'

describe('assistant message handoff', () => {
  beforeEach(() => sessionStorage.clear())

  it('keeps the first message isolated to the user and assistant scope', () => {
    const key = assistantMessageHandoffKey('user-1', 'creation', 'project-1')
    const otherKey = assistantMessageHandoffKey('user-2', 'creation', 'project-1')
    saveAssistantMessageHandoff(key, '  制定一个周末插花计划  ')

    expect(readAssistantMessageHandoff(key)).toBe('制定一个周末插花计划')
    expect(readAssistantMessageHandoff(otherKey)).toBeUndefined()

    clearAssistantMessageHandoff(key)
    expect(readAssistantMessageHandoff(key)).toBeUndefined()
  })
})
