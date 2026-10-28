const tags = new Set('p div section span article main header footer h1 h2 h3 h4 h5 h6 blockquote pre code ul ol li strong b em i u s del br hr img a table thead tbody tfoot tr th td figure figcaption sup sub'.split(' '))
const styles = new Set('color background-color font-size font-weight font-style font-family line-height text-align text-indent text-decoration letter-spacing white-space word-break overflow-wrap margin margin-top margin-right margin-bottom margin-left padding padding-top padding-right padding-bottom padding-left border border-top border-right border-bottom border-left border-color border-width border-style border-radius width max-width min-width height max-height vertical-align display list-style-type'.split(' '))

export function buildArticleDocument(title: string, html: string, images: Map<string, string>): string {
  // A template keeps untrusted nodes inert while rebuilding the small article allowlist.
  const source = document.createElement('template')
  source.innerHTML = html
  const output = document.implementation.createHTMLDocument('')
  function clean(node: Node): Node | null {
    if (node.nodeType === Node.TEXT_NODE) return output.createTextNode(node.textContent || '')
    if (!(node instanceof HTMLElement) || !tags.has(node.tagName.toLowerCase())) return null
    const element = output.createElement(node.tagName.toLowerCase())
    if (node.tagName === 'IMG') {
      const src = images.get(node.getAttribute('src') || node.getAttribute('data-src') || '')
      if (!src) return null
      element.setAttribute('src', src)
      element.setAttribute('alt', node.getAttribute('alt') || '')
      element.setAttribute('referrerpolicy', 'no-referrer')
    }
    for (const property of Array.from(node.style)) {
      const value = node.style.getPropertyValue(property)
      if (styles.has(property) && !/url\s*\(|expression|[\\@]|var\s*\(|image-set|behavior|binding/i.test(value)) element.style.setProperty(property, value)
    }
    for (const attribute of ['colspan', 'rowspan']) {
      const value = node.getAttribute(attribute)
      if (value && /^\d{1,2}$/.test(value)) element.setAttribute(attribute, value)
    }
    for (const child of Array.from(node.childNodes)) {
      const safe = clean(child)
      if (safe) element.appendChild(safe)
    }
    return element
  }
  const heading = output.createElement('h1')
  heading.textContent = title
  output.body.appendChild(heading)
  for (const node of Array.from(source.content.childNodes)) {
    const safe = clean(node)
    if (safe) output.body.appendChild(safe)
  }
  return `<!doctype html><html lang="zh-CN"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><meta name="referrer" content="no-referrer"><meta name="robots" content="noindex,nofollow"><meta http-equiv="Content-Security-Policy" content="default-src 'none'; img-src data:; style-src 'unsafe-inline'; base-uri 'none'; form-action 'none'"><style>html{color-scheme:light;background:#fff;color:#27272a;font-family:system-ui,sans-serif}body{box-sizing:border-box;max-width:720px;margin:0 auto;padding:36px 24px 72px;font-size:16px;line-height:1.85;overflow-wrap:anywhere}h1{font-size:28px;line-height:1.45;margin:0 0 32px;letter-spacing:0}img{max-width:100%!important;height:auto!important}pre{white-space:pre-wrap}table{max-width:100%;display:block;overflow:auto}a{color:inherit}@media(max-width:600px){body{padding:28px 20px 56px}h1{font-size:25px}}</style></head><body>${output.body.innerHTML}</body></html>`
}
