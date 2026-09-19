import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('ProjectsPage layout contracts', () => {
  it('keeps project visual style independent from global templates', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    const referenceUpload = source.indexOf('<ReferenceAssetUpload')
    const visualStyleField = source.indexOf('name="visual_style"')

    expect(source).toContain('supportsVisualReference')
    expect(source).not.toContain('(isSeednote || isEcommerce)')
    expect(referenceUpload).toBeGreaterThan(-1)
    expect(visualStyleField).toBeGreaterThan(-1)
    expect(referenceUpload).toBeLessThan(visualStyleField)
    expect(source).not.toContain('TemplatePicker')
    expect(source).not.toContain('selectedTemplate')
    expect(source).not.toContain('template_id')
    expect(source).not.toContain('api.templates.get')
    expect(source).not.toContain('图文视觉配置')
    expect(source).not.toContain('GUIZANG_SOCIAL_CARD_STYLE')
    expect(source).not.toContain('applyGuizangSocialCardPreset')
    expect(source).not.toContain('归藏社交卡')
    expect(source).not.toContain('Guizang social card')
    expect(source).not.toContain('社交卡片')
    expect(source).not.toContain('<TemplatePicker type="article"')
    expect(source).not.toContain('<TemplatePicker type="ecommerce"')
  })

  it('keeps Montage portrait references out of visual-style analysis', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')
    const montageGuard = source.indexOf('{!isMontage ? (')
    const analyzedField = source.indexOf('<AnalyzedImageField')

    expect(source).toContain('supportsVisualReference')
    expect(source).toContain('supportsVisualReference && isMontage')
    expect(montageGuard).toBeGreaterThan(-1)
    expect(analyzedField).toBeGreaterThan(montageGuard)
    expect(source).not.toContain("selectedPlatform !== 'seednote'")
  })

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

  it('keeps only unfinished project platforms behind the admin gate', () => {
    const source = readFileSync(join(here, 'ProjectsPage.tsx'), 'utf8')

    expect(source).toContain("new Set<ProjectPlatform>(['moments', 'ecommerce'])")
    expect(source).toContain("case 'montage':")
    expect(source).toContain('user?.is_admin === true')
    expect(source).toContain('canViewPlatform(project.platform, isAdmin)')
    expect(source).toContain('canViewPlatform(opt.value, isAdmin)')
  })
})
