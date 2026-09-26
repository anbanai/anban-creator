import type { ReactNode } from "react"
import { CircleHelp } from "lucide-react"

import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"

interface FieldHintProps {
  children: ReactNode
  /** 无障碍名称；缺省时用字符串内容兜底。 */
  label?: string
  className?: string
}

/**
 * 表单字段的补充说明入口。
 * 用于解释字段的实际影响（写入哪、影响什么），而不是重复字段名。
 */
function FieldHint({ children, label, className }: FieldHintProps) {
  const accessibleLabel = label ?? (typeof children === "string" ? children : "字段说明")

  return (
    <Tooltip>
      <TooltipTrigger
        render={
          <button
            type="button"
            className={cn(
              "inline-flex shrink-0 cursor-help text-muted-foreground transition-colors hover:text-foreground",
              className
            )}
            aria-label={accessibleLabel}
          />
        }
      >
        <CircleHelp className="size-3.5" aria-hidden="true" />
      </TooltipTrigger>
      <TooltipContent>{children}</TooltipContent>
    </Tooltip>
  )
}

export { FieldHint }
