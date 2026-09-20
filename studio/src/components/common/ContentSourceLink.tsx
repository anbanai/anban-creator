import { ExternalLink } from 'lucide-react'

type Props = {
  url?: string
  noteId?: string
  className?: string
}

// A public note ID is distinct from our database ID and an account UID.
export default function ContentSourceLink({ url, noteId, className = '' }: Props) {
  let href = ''
  const value = url?.trim()
  if (value) {
    try {
      const parsed = new URL(value)
      if (['https:', 'http:'].includes(parsed.protocol) && !parsed.username && !parsed.password) href = value
    } catch { /* Incomplete input has no navigation action. */ }
  } else if (noteId && /^[A-Za-z0-9_-]{1,100}$/.test(noteId)) {
    href = `https://www.xiaohongshu.com/explore/${encodeURIComponent(noteId)}`
  }
  if (!href) return null

  return <span className={`inline-flex max-w-full items-center gap-2 text-xs ${className}`}>
    <span className="max-w-56 truncate text-muted-foreground" title={value || noteId}>{value || `笔记 ID：${noteId}`}</span>
    <a className="inline-flex shrink-0 items-center gap-1 text-primary hover:underline" href={href} target="_blank" rel="noopener noreferrer">
      查看原文 <ExternalLink className="h-3 w-3" aria-hidden="true" />
    </a>
  </span>
}
