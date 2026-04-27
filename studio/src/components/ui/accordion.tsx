"use client"

import { Accordion } from "@base-ui/react/accordion"
import { ChevronRight } from "lucide-react"

import { cn } from "@/lib/utils"

function AccordionRoot({ className, ...props }: Accordion.Root.Props) {
  return (
    <Accordion.Root
      data-slot="accordion"
      className={cn("divide-y divide-border", className)}
      {...props}
    />
  )
}

function AccordionItem({ className, ...props }: Accordion.Item.Props) {
  return (
    <Accordion.Item
      data-slot="accordion-item"
      className={cn("group/accordion-item", className)}
      {...props}
    />
  )
}

function AccordionTrigger({ className, ...props }: Accordion.Trigger.Props) {
  return (
    <Accordion.Header>
      <Accordion.Trigger
        data-slot="accordion-trigger"
        className={cn(
          "flex w-full items-center justify-between px-4 py-3 text-sm font-medium text-foreground transition-colors hover:bg-muted/50 [&[data-panel-open]_[data-chevron]]:rotate-90",
          className
        )}
        {...props}
      >
        {props.children}
        <ChevronRight
          data-chevron
          className="h-4 w-4 shrink-0 text-muted-foreground transition-transform duration-200"
        />
      </Accordion.Trigger>
    </Accordion.Header>
  )
}

function AccordionContent({ className, ...props }: Accordion.Panel.Props) {
  return (
    <Accordion.Panel
      data-slot="accordion-content"
      className={cn("overflow-hidden px-4 pb-4 text-sm", className)}
      {...props}
    />
  )
}

export { AccordionRoot, AccordionItem, AccordionTrigger, AccordionContent }
