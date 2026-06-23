import { useEffect, useRef, useState } from 'react'
import { ImageIcon, Loader2 } from 'lucide-react'
import http from '@/lib/http-client'
import { isInternalStorageUrl, normalizeStorageUrl } from '@/lib/storage-url'
import { cn } from '@/lib/utils'

export interface SignedImageProps extends Omit<React.ImgHTMLAttributes<HTMLImageElement>, 'src' | 'loading'> {
  src: string
  /** Icon shown when src is empty or load fails. Defaults to ImageIcon. */
  fallbackIcon?: React.ReactNode
  /** className applied to the fallback wrapper. */
  fallbackClassName?: string
  /** Show a spinner while fetching internal blobs. Default true. */
  showLoading?: boolean
}

/**
 * Render an image that may live behind JWT-authenticated `/files/...` endpoints.
 *
 * Direct `<img src="/files/...">` would fail because browsers don't attach the
 * Authorization header to image requests. For internal URLs we fetch the bytes
 * via the authenticated `http` client and render the resulting blob URL.
 * External URLs (https://...) pass through unchanged.
 */
export function SignedImage({
  src,
  alt = '',
  className,
  fallbackIcon,
  fallbackClassName,
  showLoading = true,
  ...imgProps
}: SignedImageProps) {
  const [resolvedUrl, setResolvedUrl] = useState('')
  const [status, setStatus] = useState<'idle' | 'loading' | 'error'>('idle')
  const blobUrlRef = useRef('')

  useEffect(() => {
    if (!src) {
      setResolvedUrl('')
      setStatus('idle')
      return
    }

    const normalized = normalizeStorageUrl(src)

    if (!isInternalStorageUrl(normalized)) {
      // External URL — render directly.
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
      setResolvedUrl(normalized)
      setStatus('idle')
      return
    }

    let cancelled = false
    setStatus('loading')
    http
      .get(normalized, { responseType: 'blob' })
      .then((res) => {
        if (cancelled) return
        if (blobUrlRef.current) URL.revokeObjectURL(blobUrlRef.current)
        const blobUrl = URL.createObjectURL(res.data as Blob)
        blobUrlRef.current = blobUrl
        setResolvedUrl(blobUrl)
        setStatus('idle')
      })
      .catch(() => {
        if (cancelled) return
        setStatus('error')
        setResolvedUrl('')
      })

    return () => {
      cancelled = true
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [src])

  useEffect(() => {
    return () => {
      if (blobUrlRef.current) {
        URL.revokeObjectURL(blobUrlRef.current)
        blobUrlRef.current = ''
      }
    }
  }, [])

  if (!src || status === 'error') {
    return (
      <div className={cn('flex h-full w-full items-center justify-center', fallbackClassName)}>
        {fallbackIcon ?? <ImageIcon className="h-10 w-10 text-muted-foreground/40" />}
      </div>
    )
  }

  if (status === 'loading' && showLoading) {
    return (
      <div className={cn('flex h-full w-full items-center justify-center', fallbackClassName)}>
        <Loader2 className="h-6 w-6 animate-spin text-muted-foreground/60" />
      </div>
    )
  }

  return (
    <img
      src={resolvedUrl}
      alt={alt}
      className={className}
      loading="lazy"
      onError={() => setStatus('error')}
      {...imgProps}
    />
  )
}
