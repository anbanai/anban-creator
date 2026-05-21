import { screen, render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Combobox } from './Combobox'

describe('Combobox', () => {
  it('does not render a clear button inside the combobox trigger', () => {
    render(
      <Combobox
        options={[{ value: 'ch-1', label: '测试账号' }]}
        value="ch-1"
        onChange={() => {}}
      />,
    )

    expect(screen.getByRole('combobox').querySelector('button')).toBeNull()
  })
})
