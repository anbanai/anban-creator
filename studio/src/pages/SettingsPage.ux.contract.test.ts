import { readFileSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, it } from 'vitest'

const here = dirname(fileURLToPath(import.meta.url))

describe('SettingsPage readiness center contract', () => {
  it('presents settings as an integration readiness center', () => {
    const source = readFileSync(join(here, 'SettingsPage.tsx'), 'utf8')

    expect(source).toContain('接入就绪中心')
    expect(source).toContain('执行环境')
    expect(source).toContain('平台密钥')
    expect(source).not.toContain('ModelConfigSection')
    expect(source).toContain('发布渠道')
    expect(source).toContain('账号安全')
    expect(source).toContain('SettingsReadinessItem')
    expect(source).toContain('buildSettingsReadinessItems')
    expect(source).toContain('impact')
    expect(source).toContain('actionLabel')
  })
})
