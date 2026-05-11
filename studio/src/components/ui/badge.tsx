import { mergeProps } from "@base-ui/react/merge-props"
import { useRender } from "@base-ui/react/use-render"
import { cva, type VariantProps } from "class-variance-authority"

import { cn } from "@/lib/utils"

const badgeVariants = cva(
  "group/badge inline-flex h-5 w-fit shrink-0 items-center justify-center gap-1 overflow-hidden rounded-4xl border border-transparent px-2 py-0.5 text-xs font-medium whitespace-nowrap transition-all focus-visible:border-ring focus-visible:ring-[3px] focus-visible:ring-ring/50 has-data-[icon=inline-end]:pr-1.5 has-data-[icon=inline-start]:pl-1.5 aria-invalid:border-destructive aria-invalid:ring-destructive/20 dark:aria-invalid:ring-destructive/40 [&>svg]:pointer-events-none [&>svg]:size-3!",
  {
    variants: {
      variant: {
        default: "bg-primary text-primary-foreground [a]:hover:bg-primary/80",
        secondary:
          "bg-secondary text-secondary-foreground [a]:hover:bg-secondary/80",
        destructive:
          "bg-destructive/10 text-destructive focus-visible:ring-destructive/20 dark:bg-destructive/20 dark:focus-visible:ring-destructive/40 [a]:hover:bg-destructive/20",
        outline:
          "border-border text-foreground [a]:hover:bg-muted [a]:hover:text-muted-foreground",
        ghost:
          "hover:bg-muted hover:text-muted-foreground dark:hover:bg-muted/50",
        link: "text-primary underline-offset-4 hover:underline",
        success:
          "bg-[oklch(var(--success)/0.1)] text-[oklch(var(--success))] ring-1 ring-[oklch(var(--success)/0.2)] [a]:hover:bg-[oklch(var(--success)/0.2)]",
        danger:
          "bg-[oklch(var(--danger)/0.1)] text-[oklch(var(--danger))] ring-1 ring-[oklch(var(--danger)/0.2)] [a]:hover:bg-[oklch(var(--danger)/0.2)]",
        warning:
          "bg-[oklch(var(--warning)/0.1)] text-[oklch(var(--warning))] ring-1 ring-[oklch(var(--warning)/0.2)] [a]:hover:bg-[oklch(var(--warning)/0.2)]",
        info:
          "bg-[oklch(var(--info)/0.1)] text-[oklch(var(--info))] ring-1 ring-[oklch(var(--info)/0.2)] [a]:hover:bg-[oklch(var(--info)/0.2)]",
        neutral:
          "bg-muted text-muted-foreground [a]:hover:bg-muted/80",
      },
    },
    defaultVariants: {
      variant: "default",
    },
  }
)

function Badge({
  className,
  variant = "default",
  render,
  ...props
}: useRender.ComponentProps<"span"> & VariantProps<typeof badgeVariants>) {
  return useRender({
    defaultTagName: "span",
    props: mergeProps<"span">(
      {
        className: cn(badgeVariants({ variant }), className),
      },
      props
    ),
    render,
    state: {
      slot: "badge",
      variant,
    },
  })
}

export { Badge, badgeVariants }
export default Badge
