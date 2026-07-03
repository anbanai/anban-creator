import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('places visual template import before the visual style field for image-based project types', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    const visualTemplateType = source.indexOf('visualTemplateType')
    const visualTemplatePicker = source.indexOf('<TemplatePicker type={visualTemplateType}')
    const visualStyleField = source.indexOf('name="visual_style"')

    expect(visualTemplateType).toBeGreaterThan(-1)
    expect(source).toContain("isWechat ? 'article'")
    expect(source).toContain("isSeednote ? 'seednote'")
    expect(source).toContain("isEcommerce ? 'ecommerce'")
    expect(visualTemplatePicker).toBeGreaterThan(-1)
    expect(visualStyleField).toBeGreaterThan(-1)
    expect(visualTemplatePicker).toBeLessThan(visualStyleField)
    expect(source).not.toContain('<TemplatePicker type="article"')
    expect(source).not.toContain('<TemplatePicker type="ecommerce"')
  })

  it('does not hardcode Seedance video model choices in the project form', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain('api.video.models')
    expect(source).not.toContain("allowed_models: ['seedance-2.0'")
    expect(source).not.toContain('<SelectItem value="seedance-2.0"')
    expect(source).not.toContain("['seedance-2.0', 'Seedance 2.0']")
  })

  it('renames visual style copy for video projects', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain('视频风格与禁忌')
    expect(source).toContain('创作约束')
  })
})
