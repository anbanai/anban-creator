import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useParams } from 'react-router-dom'
import ReactMarkdown from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { AlertCircle, LoaderCircle, RefreshCw } from 'lucide-react'
import { Button } from '@/components/common/button'
import { readPublicShare, shareImageURL, ShareReadError } from '@/lib/api/task-shares'
import { buildArticleDocument } from '@/lib/share-reader'
import type { PublicShare } from '@/types/task-share'

type ReadState = { status: 'loading' } | { status: 'error'; code: number } | { status: 'ready'; share: PublicShare; images: Map<string, string> }

function MarkdownReader({ share, images, onImageError }: { share: PublicShare; images: Map<string, string>; onImageError: () => void }) {
  const body = useRef<HTMLDivElement>(null)
  const [used, setUsed] = useState<Set<string> | null>(null)
  useLayoutEffect(() => { setUsed(new Set(Array.from(body.current?.querySelectorAll('img') || []).map((image) => image.src))) }, [share])
  return <article className="mx-auto w-full max-w-[720px] px-5 pb-16 pt-9 sm:px-8 sm:pt-12">
    <h1 className="mb-8 break-words text-[25px] font-semibold leading-[1.45] text-zinc-900 sm:text-[28px]">{share.title}</h1>
    <div ref={body} className="prose prose-zinc max-w-none break-words leading-[1.85] prose-headings:font-semibold prose-img:mx-auto prose-img:h-auto prose-img:max-w-full prose-img:rounded-md prose-pre:whitespace-pre-wrap prose-pre:break-words">
      <ReactMarkdown remarkPlugins={[remarkGfm]} skipHtml urlTransform={(url) => images.get(url) || ''} components={{
        img: ({ src, alt }) => src ? <img src={src} alt={alt || ''} referrerPolicy="no-referrer" onError={onImageError} /> : null,
        a: ({ children }) => <span>{children}</span>,
      }}>{share.body}</ReactMarkdown>
    </div>
    {used ? <div className="mt-8 space-y-6">{share.images.filter((image) => images.has(image.url) && !used.has(images.get(image.url)!)).map((image) => <img key={image.id} src={images.get(image.url)} alt={image.alt} referrerPolicy="no-referrer" onError={onImageError} className="mx-auto block h-auto max-w-full rounded-md" />)}</div> : null}
  </article>
}

function useReaderMetadata(title: string) {
  useEffect(() => {
    const originalTitle = document.title
    const saved = ['robots', 'referrer'].map((name) => {
      const existing = document.querySelector<HTMLMetaElement>(`meta[name="${name}"]`)
      const element = existing || document.createElement('meta')
      const content = element.getAttribute('content')
      element.name = name
      element.content = name === 'robots' ? 'noindex, nofollow' : 'no-referrer'
      if (!existing) document.head.appendChild(element)
      return { element, existing, content }
    })
    document.title = title
    return () => {
      document.title = originalTitle
      for (const { element, existing, content } of saved) {
        if (!existing) element.remove()
        else if (content === null) element.removeAttribute('content')
        else element.content = content
      }
    }
  }, [title])
}

export default function PublicSharePage() {
  const { token = '' } = useParams()
  const [state, setState] = useState<ReadState>({ status: 'loading' })
  const [revision, setRevision] = useState(0)
  const [imageError, setImageError] = useState(false)
  const [decodedImages, setDecodedImages] = useState<Set<string>>(new Set())
  useReaderMetadata(state.status === 'ready' ? state.share.title : '分享作品 · Anban AI')

  useEffect(() => {
    const controller = new AbortController()
    setState({ status: 'loading' }); setImageError(false); setDecodedImages(new Set())
    async function load() {
      try {
        const share = await readPublicShare(token, controller.signal)
        if (!Number.isFinite(Date.parse(share.expires_at)) || Date.parse(share.expires_at) <= Date.now()) throw new ShareReadError(410)
        const images = new Map<string, string>()
        for (const image of share.images) {
          const url = shareImageURL(image.url, token)
          if (!url) throw new ShareReadError(409)
          if (share.format === 'html') {
            const response = await fetch(url, { cache: 'no-store', credentials: 'omit', referrerPolicy: 'no-referrer', signal: controller.signal })
            if (!response.ok) throw new ShareReadError(response.status === 404 || response.status === 410 ? response.status : 409)
            const mime = response.headers.get('Content-Type')?.split(';')[0].trim().toLowerCase() || ''
            const bytes = new Uint8Array(await response.arrayBuffer())
            if (!/^image\/(png|jpeg|gif|webp|avif)$/.test(mime) || !bytes.length) throw new ShareReadError(409)
            if (controller.signal.aborted) return
            let binary = ''
            for (let offset = 0; offset < bytes.length; offset += 8192) binary += String.fromCharCode(...bytes.subarray(offset, offset + 8192))
            images.set(image.url, `data:${mime};base64,${btoa(binary)}`)
          } else images.set(image.url, url)
        }
        if (Date.parse(share.expires_at) <= Date.now()) throw new ShareReadError(410)
        if (!controller.signal.aborted) setState({ status: 'ready', share, images })
      } catch (error) {
        if (!controller.signal.aborted) setState({ status: 'error', code: error instanceof ShareReadError ? error.status : 500 })
      }
    }
    void load()
    return () => { controller.abort() }
  }, [token, revision])

  useEffect(() => {
    const revisit = (event: PageTransitionEvent) => { if (event.persisted) setRevision((value) => value + 1) }
    window.addEventListener('pageshow', revisit)
    return () => window.removeEventListener('pageshow', revisit)
  }, [])

  useEffect(() => {
    if (state.status !== 'ready') return
    const expires = Date.parse(state.share.expires_at)
    const timeout = window.setTimeout(() => {
      if (Date.now() >= expires) setState({ status: 'error', code: 410 })
      else setRevision((value) => value + 1)
    }, Math.min(Math.max(expires - Date.now(), 0), 2_147_483_647))
    return () => window.clearTimeout(timeout)
  }, [state])

  const article = useMemo(() => state.status === 'ready' && state.share.format === 'html' ? buildArticleDocument(state.share.title, state.share.body, state.images) : '', [state])
  const isolated = state.status === 'ready' && state.share.format === 'html' && !imageError
  const invalid = state.status === 'error' && (state.code === 404 || state.code === 410)
  const unavailable = state.status === 'error' && state.code === 409
  const imagesPending = isolated && state.status === 'ready' && decodedImages.size < state.images.size
  return <main className={`${isolated ? 'flex h-dvh flex-col overflow-hidden' : 'min-h-dvh'} bg-white text-zinc-800`}>
    <header className="shrink-0 border-b border-zinc-100 px-5 py-4">
      <div className="mx-auto flex max-w-[720px] items-center justify-between gap-3 sm:px-3"><span className="text-sm font-semibold text-zinc-700">Anban AI<span className="ml-3 border-l border-zinc-200 pl-3 text-xs font-normal text-zinc-400">作品分享</span></span>{state.status === 'ready' ? <span className="text-xs text-zinc-400">{state.share.type === 'seednote' ? '种草笔记' : '公众号文章'}</span> : null}</div>
    </header>
    {state.status === 'loading' || imagesPending ? <div role="status" className="flex items-center justify-center gap-2 py-24 text-sm text-zinc-500"><LoaderCircle className="size-4 animate-spin" />正在加载作品…</div> : state.status === 'error' || imageError ? <div className="mx-auto max-w-sm px-6 py-24 text-center">
      <AlertCircle className="mx-auto mb-5 size-8 text-zinc-400" aria-hidden="true" />
      <h1 className="text-xl font-semibold">{imageError ? '图片加载失败' : invalid ? '分享链接已失效' : unavailable ? '内容暂不可用' : '暂时无法加载'}</h1>
      <p className="mt-3 text-sm leading-6 text-zinc-500">{imageError ? '图文可能已更新，请重新加载完整作品。' : invalid ? '链接可能已到期或被关闭，请联系分享者。' : unavailable ? '作品文件暂时无法读取，请稍后重试。' : '网络或服务暂时繁忙，请稍后重试。'}</p>
      {!invalid ? <Button className="mt-6" variant="outline" size="sm" onClick={() => setRevision((value) => value + 1)}><RefreshCw className="size-4" />重新加载</Button> : null}
    </div> : state.status === 'ready' ? state.share.format === 'html' ? <>
      <iframe title={state.share.title} srcDoc={article} sandbox="" referrerPolicy="no-referrer" className="min-h-0 w-full flex-1 border-0 bg-white" />
    </> : <MarkdownReader share={state.share} images={state.images} onImageError={() => setImageError(true)} /> : null}
    {isolated && state.status === 'ready' ? <div hidden aria-hidden="true">{Array.from(state.images, ([url, data]) => <img key={url} src={data} alt="" onLoad={() => setDecodedImages((previous) => new Set([...previous, url]))} onError={() => setImageError(true)} />)}</div> : null}
  </main>
}
