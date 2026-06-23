import { useQuery } from '@tanstack/react-query'
import { Loader2 } from 'lucide-react'
import { api } from '@/lib/api'

interface ThemePreviewProps {
  /** Theme resource key (e.g. "autumn-warm"). Empty → render nothing. */
  theme: string
  className?: string
}

const PREVIEW_HEIGHT = 440

/**
 * Renders a sandboxed iframe preview of a 排版样式 (theme) by fetching the
 * server-rendered WeChat HTML (built-in sample markdown + theme, deterministic).
 * Used in the article template form and read-only preview. Empty theme → null.
 */
export function ThemePreview({ theme, className }: ThemePreviewProps) {
  const { data: html, isLoading, isError } = useQuery({
    queryKey: ['theme-preview', theme],
    queryFn: () => api.resources.previewTheme(theme),
    enabled: !!theme,
    staleTime: Infinity,
  })

  if (!theme) return null

  if (isLoading) {
    return (
      <div
        className={`flex items-center justify-center rounded-md border border-input bg-muted/30 ${className ?? ''}`}
        style={{ height: PREVIEW_HEIGHT }}
      >
        <Loader2 className="h-5 w-5 animate-spin text-muted-foreground" />
      </div>
    )
  }

  if (isError || !html) {
    return (
      <div
        className={`flex items-center justify-center rounded-md border border-input text-sm text-muted-foreground ${className ?? ''}`}
        style={{ height: PREVIEW_HEIGHT }}
      >
        排版预览加载失败，请重试
      </div>
    )
  }

  return (
    <iframe
      title="排版预览"
      srcDoc={html}
      // sandbox="" is the most restrictive mode: renders HTML/CSS but blocks
      // scripts, forms, same-origin access. The theme HTML is inline-CSS only
      // (WeChat-safe), so this is safe and sufficient.
      sandbox=""
      className={`w-full rounded-md border border-input bg-white ${className ?? ''}`}
      style={{ height: PREVIEW_HEIGHT }}
    />
  )
}
