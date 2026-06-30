import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('places article visual template import before the visual style field', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    const articleTemplatePicker = source.indexOf('<TemplatePicker type="article"')
    const visualStyleField = source.indexOf('name="visual_style"')

    expect(articleTemplatePicker).toBeGreaterThan(-1)
    expect(visualStyleField).toBeGreaterThan(-1)
    expect(articleTemplatePicker).toBeLessThan(visualStyleField)
  })
})
