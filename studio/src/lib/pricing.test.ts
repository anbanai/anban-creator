import { describe, expect, it } from 'vitest'

import { taskCostFor } from './pricing'

describe('taskCostFor', () => {
  it('falls back to 3000 credits for moments', () => {
    expect(taskCostFor(undefined, 'moments')).toBe(3000)
  })
})
