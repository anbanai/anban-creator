import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('keeps visual settings flat and free of social-card preset jargon', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    const visualTemplateType = source.indexOf('visualTemplateType')
    const visualTemplatePicker = source.indexOf('<TemplatePicker type={visualTemplateType}')
    const referenceUpload = source.indexOf('<ReferenceAssetUpload')
    const visualStyleField = source.indexOf('name="visual_style"')

    expect(visualTemplateType).toBeGreaterThan(-1)
    expect(source).toContain("isWechat ? 'article'")
    expect(source).toContain("isSeednote ? 'seednote'")
    expect(source).toContain("isEcommerce ? 'ecommerce'")
    expect(source).toContain('supportsVisualReference')
    expect(source).not.toContain('(isSeednote || isEcommerce)')
    expect(visualTemplatePicker).toBeGreaterThan(-1)
    expect(referenceUpload).toBeGreaterThan(-1)
    expect(visualStyleField).toBeGreaterThan(-1)
    expect(referenceUpload).toBeLessThan(visualStyleField)
    expect(visualTemplatePicker).toBeLessThan(visualStyleField)
    expect(source).not.toContain('图文视觉配置')
    expect(source).not.toContain('GUIZANG_SOCIAL_CARD_STYLE')
    expect(source).not.toContain('applyGuizangSocialCardPreset')
    expect(source).not.toContain('归藏社交卡')
    expect(source).not.toContain('Guizang social card')
    expect(source).not.toContain('社交卡片')
    expect(source).not.toContain('<TemplatePicker type="article"')
    expect(source).not.toContain('<TemplatePicker type="ecommerce"')
  })

  it('auto-analyzes uploaded reference images for every image-based project type', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain('supportsVisualReference')
    expect(source).toContain('!supportsVisualReference')
    expect(source).not.toContain("selectedPlatform !== 'seednote'")
  })

  it('keeps project cards focused on creation readiness', () => {
    const cardSource = readFileSync(join(here, '../components/ProjectCard.tsx'), 'utf8')
    const pageSource = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(cardSource).toContain('buildProjectReadinessSummary')
    expect(cardSource).toContain('用此项目创建任务')
    expect(pageSource).toContain('createTaskHref')
  })
})
