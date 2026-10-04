import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('keeps project cards focused on project identity and basic activity', () => {
    const cardSource = readFileSync(join(here, '../components/ProjectCard.tsx'), 'utf8')
    const pageSource = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(cardSource).not.toContain('buildProjectReadinessSummary')
    expect(cardSource).not.toContain('还有配置可补齐')
    expect(cardSource).not.toContain('onCreateTask')
    expect(cardSource).not.toContain('onDelete')
    expect(cardSource).not.toContain('成功率')
    expect(pageSource).not.toContain('createTaskHref')
    expect(pageSource).not.toContain('deleteMutation')
  })

})
