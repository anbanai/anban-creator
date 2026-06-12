import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const root = resolve(import.meta.dirname, '../..')

function read(path: string) {
  return readFileSync(resolve(root, path), 'utf8')
}

function readDesignerChrome() {
  const designerDir = resolve(root, 'src/components/designer')
  const componentFiles = readdirSync(designerDir)
    .filter((file) => file.endsWith('.tsx'))
    .map((file) => read(`src/components/designer/${file}`))

  return [read('src/pages/DesignerPage.tsx'), ...componentFiles].join('\n')
}

function darkTokenLightness(css: string, token: string) {
  const darkBlock = css.match(/\.dark\s*\{([\s\S]*?)\n\}/)?.[1] ?? ''
  const match = darkBlock.match(new RegExp(`--${token}:\\s*oklch\\((\\d*\\.?\\d+)`))

  expect(match, `Expected .dark token --${token} to use oklch()`).not.toBeNull()
  return Number(match![1])
}

describe('dark mode clarity', () => {
  it('keeps core dark tokens readable enough for long studio sessions', () => {
    const css = read('src/index.css')

    expect(darkTokenLightness(css, 'muted-foreground')).toBeGreaterThanOrEqual(0.68)
    expect(darkTokenLightness(css, 'border')).toBeGreaterThanOrEqual(0.27)
    expect(darkTokenLightness(css, 'input')).toBeGreaterThanOrEqual(0.29)
    expect(darkTokenLightness(css, 'sidebar-border')).toBeGreaterThanOrEqual(0.24)
  })

  it('avoids low-contrast Designer chrome in dark mode', () => {
    const files = readDesignerChrome()

    expect(files).not.toContain('text-muted-foreground/50')
    expect(files).not.toContain('placeholder:text-muted-foreground/50')
    expect(files).not.toContain('border-border/30')
    expect(files).not.toContain('dark:bg-background/40')
  })
})
