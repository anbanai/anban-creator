import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('agent-readable plugin guides', () => {
  it.each(['claude', 'codex'])('ships a public /%s installation page', (client) => {
    const html = readFileSync(resolve(process.cwd(), `public/${client}/index.html`), 'utf8')
    expect(html).toContain('https://github.com/anbanai/creator-skills.git')
    expect(html).toContain('ANBAN_API_KEY')
    expect(html).toContain('https://creator.anbanai.com/settings#api-key-settings')
    expect(html).toContain('Do not include the API key')
    if (client === 'codex') {
      expect(html).toContain('codex plugin add anban@anbanai')
      expect(html).not.toContain('codex plugin install')
    }
  })
})
