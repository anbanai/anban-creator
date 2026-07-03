import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('places visual template import before the visual style field for image-based project types', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    const visualTemplateType = source.indexOf('visualTemplateType')
    const visualConfigSection = source.indexOf('图文视觉配置')
    const visualTemplatePicker = source.indexOf('<TemplatePicker type={visualTemplateType}')
    const referenceUpload = source.indexOf('<ReferenceImageUpload')
    const visualStyleField = source.indexOf('name="visual_style"')

    expect(visualTemplateType).toBeGreaterThan(-1)
    expect(source).toContain("isWechat ? 'article'")
    expect(source).toContain("isSeednote ? 'seednote'")
    expect(source).toContain("isEcommerce ? 'ecommerce'")
    expect(source).toContain('supportsVisualReference')
    expect(source).not.toContain('(isSeednote || isEcommerce)')
    expect(visualConfigSection).toBeGreaterThan(-1)
    expect(visualTemplatePicker).toBeGreaterThan(-1)
    expect(referenceUpload).toBeGreaterThan(-1)
    expect(visualStyleField).toBeGreaterThan(-1)
    expect(visualConfigSection).toBeLessThan(visualTemplatePicker)
    expect(visualTemplatePicker).toBeLessThan(visualStyleField)
    expect(referenceUpload).toBeLessThan(visualStyleField)
    expect(source).not.toContain('<TemplatePicker type="article"')
    expect(source).not.toContain('<TemplatePicker type="ecommerce"')
  })

  it('auto-analyzes uploaded reference images for every image-based project type', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain('supportsVisualReference')
    expect(source).toContain('!supportsVisualReference')
    expect(source).not.toContain("selectedPlatform !== 'seednote'")
  })

  it('does not hardcode Seedance video model choices in the project form', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain('api.video.models')
    expect(source).toContain('videoModelDisplayName')
    expect(source).not.toContain('model.display_name || model.key')
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
