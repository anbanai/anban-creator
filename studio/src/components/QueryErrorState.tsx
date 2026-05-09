import { AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/Button'

interface QueryErrorStateProps {
  error?: unknown
  onRetry?: () => void
  title?: string
  description?: string
}

export default function QueryErrorState({
  onRetry,
  title = '加载失败',
  description = '请检查网络连接后重试。',
}: QueryErrorStateProps) {
  return (
    <div className="flex items-center justify-center py-16">
      <div className="flex flex-col items-center gap-3 text-center">
        <div className="flex h-12 w-12 items-center justify-center rounded-full bg-destructive/10">
          <AlertCircle className="h-6 w-6 text-destructive" />
        </div>
        <p className="text-sm font-medium text-foreground">{title}</p>
        <p className="max-w-sm text-xs text-muted-foreground">{description}</p>
        {onRetry && (
          <Button variant="outline" size="sm" onClick={onRetry}>
            重试
          </Button>
        )}
      </div>
    </div>
  )
}
