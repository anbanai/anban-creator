import { describe, expect, it } from 'vitest'
import { buildArticleDocument } from './share-reader'

describe('share HTML boundary', () => {
  it('reads complete Server HTML documents with head/body wrappers without losing the article', () => {
    const result = buildArticleDocument('文章标题', '<!doctype html><html><head><title>文件标题</title><style>body{display:none}</style></head><body><section style="padding:12px"><p>第一段</p><img src="/safe"><p>最后一段</p></section></body></html>', new Map([['/safe', 'blob:safe']]))
    const doc = new DOMParser().parseFromString(result, 'text/html')
    expect(doc.querySelector('h1')?.textContent).toBe('文章标题')
    expect(doc.querySelector('section')?.textContent).toBe('第一段最后一段')
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('blob:safe')
    expect(result).not.toContain('display:none')
    expect(result).not.toContain('文件标题')
  })
  it('preserves article typography and only exact allowed images while removing active elements and CSS', () => {
    const result = buildArticleDocument('标题 <script>', '<base href="https://evil.test"><meta http-equiv="refresh" content="0;url=https://evil.test"><section style="color:rgb(1,2,3);padding:12px;position:fixed;background:url(https://evil.test)"><p onclick="bad()">正文</p><img src="/safe" onerror="bad()"><img src="/unsafe"><svg><a href="javascript:bad()">坏</a></svg><iframe src="https://evil.test"></iframe><form><input></form><a href="javascript:bad()">链接</a></section>', new Map([['/safe', 'blob:safe']] ))
    const doc = new DOMParser().parseFromString(result, 'text/html')
    expect(doc.querySelectorAll('script,iframe,svg,form,base,input')).toHaveLength(0)
    expect(doc.querySelector('section')?.style.color).toBe('rgb(1, 2, 3)')
    expect(doc.querySelector('section')?.style.padding).toBe('12px')
    expect(doc.querySelector('section')?.style.position).toBe('')
    expect(doc.querySelectorAll('img')).toHaveLength(1)
    expect(doc.querySelector('img')?.getAttribute('src')).toBe('blob:safe')
    expect(doc.querySelector('[onclick],[onerror],[href]')).toBeNull()
    expect(doc.querySelector('h1')?.textContent).toBe('标题 <script>')
    expect(result).not.toContain('evil.test')
  })
})
