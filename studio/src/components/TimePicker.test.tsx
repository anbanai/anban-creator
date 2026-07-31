import { render, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import TimePicker from './TimePicker'

describe('TimePicker', () => {
  it('preserves an exact server-provided minute instead of snapping it', async () => {
    const onChange = vi.fn()

    render(<TimePicker value="12:07" onChange={onChange} />)

    await waitFor(() => expect(onChange).not.toHaveBeenCalled())
  })
})
