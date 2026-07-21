import { describe, expect, it } from 'vitest'

import { taskCostFor } from './pricing'

describe('taskCostFor', () => {
  it('does not invent a local price when the active catalog is unavailable', () => {
    expect(taskCostFor(undefined, 'moments')).toBeUndefined()
  })
})
