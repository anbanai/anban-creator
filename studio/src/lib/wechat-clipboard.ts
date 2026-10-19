import type { TaskFile } from '@/types'

const MAX_CLIPBOARD_BYTES = 8 * 1024 * 1024
const SAFE_TAGS = new Set(['A', 'B', 'BLOCKQUOTE', 'BR', 'CODE', 'DIV', 'EM', 'H1', 'H2', 'H3', 'H4', 'HR', 'IMG', 'LI', 'OL', 'P', 'PRE', 'S', 'SPAN', 'STRONG', 'TABLE', 'TBODY', 'TD', 'TH', 'THEAD', 'TR', 'UL'])
const SAFE_STYLE_PROPERTIES = new Set(['background-color', 'border', 'border-bottom', 'border-left', 'border-right', 'border-top', 'color', 'display', 'font-family', 'font-size', 'font-style', 'font-weight', 'height', 'line-height', 'margin', 'margin-bottom', 'margin-left', 'margin-right', 'margin-top', 'max-width', 'padding', 'padding-bottom', 'padding-left', 'padding-right', 'padding-top', 'text-align', 'text-decoration', 'text-indent', 'vertical-align', 'white-space', 'width'])

function sanitizeInlineStyle(source: string): string {
  const probe = document.createElement('span')
  probe.style.cssText = source
  const safe: string[] = []
  for (const property of Array.from(probe.style)) {
    const value = probe.style.getPropertyValue(property).trim()
    if (!SAFE_STYLE_PROPERTIES.has(property) || /url\s*\(|expression\s*\(|javascript:|@import|behavior\s*:/i.test(value)) continue
    safe.push(`${property}:${value}`)
  }
  return safe.join(';')
}

export function sanitizeWechatHTML(source: string): string {
  const document = new DOMParser().parseFromString(source, 'text/html')
  for (const element of Array.from(document.body.querySelectorAll('script,style,iframe,form,input,link,object,embed,svg,math'))) element.remove()
  for (const element of Array.from(document.body.querySelectorAll('*'))) {
    if (!SAFE_TAGS.has(element.tagName)) {
      element.replaceWith(...Array.from(element.childNodes))
      continue
    }
    for (const attribute of Array.from(element.attributes)) {
      const name = attribute.name.toLowerCase()
      if (name === 'style') {
        const style = sanitizeInlineStyle(attribute.value)
        if (style) element.setAttribute('style', style)
        else element.removeAttribute('style')
        continue
      }
      if ((element.tagName === 'A' && name === 'href') || (element.tagName === 'IMG' && ['src', 'alt', 'width', 'height'].includes(name))) continue
      element.removeAttribute(attribute.name)
    }
    if (element.tagName === 'A') {
      const href = (element.getAttribute('href') || '').trim()
      if (href && !/^(?:https?:\/\/|mailto:|tel:|#|\/)/i.test(href)) element.removeAttribute('href')
    }
    if (element.tagName === 'IMG') {
      const src = element.getAttribute('src') || ''
      if (!/^data:image\/(?:png|jpeg|gif|webp);base64,/i.test(src) && !/^https?:\/\//i.test(src) && !src.startsWith('/')) element.removeAttribute('src')
    }
  }
  return document.body.innerHTML
}

function plainTextFromHTML(html: string): string {
  const document = new DOMParser().parseFromString(html, 'text/html')
  return document.body.innerText || document.body.textContent || ''
}

function sameTaskFileReference(source: string, reference: string | undefined): boolean {
  const left = source.trim()
  const right = (reference || '').trim()
  if (!left || !right) return false
  if (left === right) return true
  try {
    const base = typeof window !== 'undefined' ? window.location.origin : 'http://localhost'
    const sourceURL = new URL(left, base)
    const referenceURL = new URL(right, base)
    // Signed storage URLs are re-issued when the Studio reloads task files.
    // The stable owned object path identifies the structured TaskFile; query
    // parameters only carry a short-lived signature.
    return sourceURL.origin === referenceURL.origin && sourceURL.pathname === referenceURL.pathname
  } catch {
    return false
  }
}

async function prepareArticleHTML(source: string, files: TaskFile[], fetchFile: (fileId: string) => Promise<Blob>, maxBytes: number): Promise<string> {
  const document = new DOMParser().parseFromString(sanitizeWechatHTML(source), 'text/html')
  const images = Array.from(document.body.querySelectorAll('img'))
  if (new Blob([document.body.innerHTML]).size > maxBytes) throw new Error('文章和图片超过剪贴板容量，请下载 HTML 后手动处理')
  for (const image of images) {
    const src = (image.getAttribute('src') || '').trim()
    const file = files.find((candidate) => candidate.mime_type?.startsWith('image/') && [candidate.url, candidate.preview_url, candidate.download_url, candidate.wechat_url].some((reference) => sameTaskFileReference(src, reference)))
    if (!file) throw new Error(`找不到图片素材：${src || '图片缺少来源'}`)
    const blob = await fetchFile(file.id)
    image.src = `data:${blob.type || file.mime_type};base64,${await blobToBase64(blob)}`
    if (new Blob([document.body.innerHTML]).size > maxBytes) throw new Error('文章和图片超过剪贴板容量，请下载 HTML 后手动处理')
  }
  return document.body.innerHTML
}

export function prepareWechatArticleHTML(source: string, files: TaskFile[], fetchFile: (fileId: string) => Promise<Blob>): Promise<string> {
  return prepareArticleHTML(source, files, fetchFile, MAX_CLIPBOARD_BYTES)
}

export function prepareWechatArticleHTMLForDownload(source: string, files: TaskFile[], fetchFile: (fileId: string) => Promise<Blob>): Promise<string> {
  return prepareArticleHTML(source, files, fetchFile, Number.POSITIVE_INFINITY)
}

export function downloadWechatArticleHTML(html: string, filename = 'wechat-article.html'): void {
  const url = URL.createObjectURL(new Blob([sanitizeWechatHTML(html)], { type: 'text/html;charset=utf-8' }))
  const link = document.createElement('a')
  link.href = url
  link.download = filename
  link.click()
  URL.revokeObjectURL(url)
}

function blobToBase64(blob: Blob): Promise<string> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader()
    reader.onload = () => resolve(String(reader.result).split(',', 2)[1] || '')
    reader.onerror = () => reject(reader.error || new Error('读取图片失败'))
    reader.readAsDataURL(blob)
  })
}

function writeWechatRichTextFallback(safeHTML: string): void {
  const wrapper = document.createElement('div')
  wrapper.contentEditable = 'true'
  wrapper.setAttribute('aria-hidden', 'true')
  Object.assign(wrapper.style, { position: 'fixed', left: '-10000px', top: '0', opacity: '0' })
  wrapper.innerHTML = safeHTML
  document.body.appendChild(wrapper)
  const selection = window.getSelection()
  const range = document.createRange()
  range.selectNodeContents(wrapper)
  selection?.removeAllRanges()
  selection?.addRange(range)
  const copied = document.execCommand('copy')
  selection?.removeAllRanges()
  wrapper.remove()
  if (!copied) throw new Error('当前浏览器不支持富文本复制，请下载 HTML 文件')
}

export async function writeWechatRichText(html: string): Promise<void> {
  const safeHTML = sanitizeWechatHTML(html)
  const text = plainTextFromHTML(safeHTML)
  if (navigator.clipboard?.write && typeof ClipboardItem !== 'undefined') {
    try {
      await navigator.clipboard.write([new ClipboardItem({
        'text/html': new Blob([safeHTML], { type: 'text/html' }),
        'text/plain': new Blob([text], { type: 'text/plain' }),
      })])
      return
    } catch {
      // Fall through for browsers that expose the API but reject rich HTML.
    }
  }
  writeWechatRichTextFallback(safeHTML)
}

export async function copyWechatArticle(source: string, files: TaskFile[], fetchFile: (fileId: string) => Promise<Blob>): Promise<void> {
  if (!navigator.clipboard?.write || typeof ClipboardItem === 'undefined') {
    await writeWechatRichText(await prepareWechatArticleHTML(source, files, fetchFile))
    return
  }

  const preparedHTMLPromise = prepareWechatArticleHTML(source, files, fetchFile)
  let resolveHTML!: (blob: Blob) => void
  let resolveText!: (blob: Blob) => void
  let rejectHTML!: (error: unknown) => void
  const htmlBlob = new Promise<Blob>((resolve, reject) => { resolveHTML = resolve; rejectHTML = reject })
  const textBlob = new Promise<Blob>((resolve) => { resolveText = resolve })
  const preparePromise = preparedHTMLPromise.then((html) => {
    resolveHTML(new Blob([html], { type: 'text/html' }))
    resolveText(new Blob([plainTextFromHTML(html)], { type: 'text/plain' }))
  }, (error) => {
    rejectHTML(error)
    throw error
  })
  try {
    const clipboardItem = new ClipboardItem({
      'text/html': htmlBlob as unknown as Blob,
      'text/plain': textBlob as unknown as Blob,
    })
    const writePromise = navigator.clipboard.write([clipboardItem])
    await Promise.all([writePromise, preparePromise])
  } catch {
    // Some Safari/Firefox versions expose ClipboardItem but reject async HTML
    // payloads. Reuse the prepared content through the selection-based path.
    const html = await preparedHTMLPromise
    writeWechatRichTextFallback(sanitizeWechatHTML(html))
  }
}

export async function writeWechatPlainText(value: string): Promise<void> {
  if (navigator.clipboard?.writeText) {
    try {
      await navigator.clipboard.writeText(value)
      return
    } catch {
      // Fall through for browsers that expose the API but reject the write.
    }
  }
  const input = document.createElement('textarea')
  input.value = value
  input.style.position = 'fixed'
  input.style.left = '-10000px'
  document.body.appendChild(input)
  input.select()
  const copied = document.execCommand('copy')
  input.remove()
  if (!copied) throw new Error('当前浏览器不支持复制')
}

export async function articleMetadataFromFiles(files: TaskFile[], fetchFile: (fileId: string) => Promise<Blob>): Promise<{ title: string; digest: string; author: string }> {
  const artifact = files.find((file) => /(?:^|\/)draft\.json$/i.test(file.file_name))
  if (!artifact) return { title: '', digest: '', author: '' }
  const payload = JSON.parse(await (await fetchFile(artifact.id)).text()) as { article?: Record<string, unknown> }
  const article = payload.article || {}
  return {
    title: typeof article.title === 'string' ? article.title : '',
    digest: typeof article.digest === 'string' ? article.digest : '',
    author: typeof article.author === 'string' ? article.author : '',
  }
}

export function articleMetadataValue(metadata: { title: string; digest: string; author: string }, field: 'title' | 'digest' | 'author', fallbackAuthor = ''): string {
  return field === 'author' ? metadata.author || fallbackAuthor : metadata[field]
}
