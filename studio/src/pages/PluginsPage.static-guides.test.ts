import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

describe('agent-readable plugin guides', () => {
  it.each(['claude', 'codex', 'dsh'])('ships a public /%s installation page', (client) => {
    const html = readFileSync(resolve(process.cwd(), `public/${client}/index.html`), 'utf8')
    expect(html).toContain('https://github.com/anbanai/creator-harness.git')
    expect(html).toContain('ANBAN_API_KEY')
    expect(html).toContain('https://creator.anbanai.com/settings#api-key-settings')
    expect(html).toContain('Do not include the API key')
    if (client === 'codex') {
      expect(html).toContain('codex plugin add anban@anbanai')
      expect(html).not.toContain('codex plugin install')
    }
  })

  it('installs the DSH plugin as an exact published version through two explicit steps', () => {
    const html = readFileSync(resolve(process.cwd(), 'public/dsh/index.html'), 'utf8')
    expect(html).toContain('dsh plugin --profile "$ACTIVE_PROFILE" add "@anban/dsh-plugin@${PUBLISHED_VERSION}"')
    expect(html).toContain('dsh --profile "$ACTIVE_PROFILE" --dump-config')
    expect(html).toContain('dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh install-presets')
    expect(html).toContain('dsh plugin --profile "$ACTIVE_PROFILE" exec anban-dsh status')
    expect(html).toContain('https://creator.anbanai.com/mcp')
    expect(html).toContain('$DSH_HOME/.credentials.yaml')
    expect(html).not.toContain('@anban/dsh-plugin@latest')
  })
})
