import {
  Pagination,
  PaginationContent,
  PaginationItem,
  PaginationPrevious,
  PaginationNext,
} from "@/components/ui/Pagination"

interface SimplePaginationProps {
  page: number
  totalPages: number
  onPageChange: (page: number) => void
}

export function SimplePagination({ page, totalPages, onPageChange }: SimplePaginationProps) {
  return (
    <Pagination>
      <PaginationContent>
        <PaginationItem>
          <PaginationPrevious
            text="上一页"
            onClick={(e) => {
              if (page > 1) onPageChange(page - 1)
              else e.preventDefault()
            }}
            className={page <= 1 ? "pointer-events-none opacity-50" : "cursor-pointer"}
          />
        </PaginationItem>
        <PaginationItem>
          <PaginationNext
            text="下一页"
            onClick={(e) => {
              if (page < totalPages) onPageChange(page + 1)
              else e.preventDefault()
            }}
            className={page >= totalPages ? "pointer-events-none opacity-50" : "cursor-pointer"}
          />
        </PaginationItem>
      </PaginationContent>
    </Pagination>
  )
}
