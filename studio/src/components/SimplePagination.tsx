import { ChevronLeft, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'

interface SimplePaginationProps {
  page: number
  totalPages: number
  onPageChange: (page: number) => void
}

export function SimplePagination({ page, totalPages, onPageChange }: SimplePaginationProps) {
  return (
    <nav aria-label="分页" className="flex flex-wrap items-center justify-center gap-3">
      <Button variant="outline" size="sm" disabled={page <= 1} onClick={() => onPageChange(page - 1)}>
        <ChevronLeft aria-hidden="true" />上一页
      </Button>
      <span className="min-w-24 text-center text-sm tabular-nums text-muted-foreground" aria-live="polite" aria-atomic="true">
        第 {page} / {Math.max(1, totalPages)} 页
      </span>
      <Button variant="outline" size="sm" disabled={page >= totalPages} onClick={() => onPageChange(page + 1)}>
        下一页<ChevronRight aria-hidden="true" />
      </Button>
    </nav>
  )
}
