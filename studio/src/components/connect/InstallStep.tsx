import { useState } from 'react'
import { ChevronRight } from 'lucide-react'
import { Collapsible, CollapsibleTrigger, CollapsibleContent } from '@/components/ui/collapsible'
import CodeBlock from '@/components/connect/CodeBlock'
import StepCard from '@/components/connect/StepCard'
import { cn } from '@/lib/utils'

interface InstallStepProps {
  cliName: string
  oneLiner: string
  oneLinerHint: string
  advancedCli: string
  advancedHint?: string
}

export default function InstallStep({
  cliName,
  oneLiner,
  oneLinerHint,
  advancedCli,
  advancedHint,
}: InstallStepProps) {
  const [open, setOpen] = useState(false)

  return (
    <StepCard step={3} title="安装插件">
      <div className="space-y-3">
        <div className="space-y-1">
          <p className="text-xs font-medium text-foreground">推荐 · 告诉 AI 一句话</p>
          <p className="text-xs text-muted-foreground">
            在 {cliName} 里直接对 AI 说：
          </p>
          <CodeBlock code={oneLiner} language="text" />
          <p className="text-xs text-muted-foreground">{oneLinerHint}</p>
        </div>

        <Collapsible open={open} onOpenChange={setOpen}>
          <CollapsibleTrigger
            className={cn(
              'group flex w-full items-center gap-1 rounded-md px-1 py-1 text-xs font-medium text-muted-foreground transition-colors hover:text-foreground',
            )}
          >
            <ChevronRight
              className={cn(
                'h-3 w-3 transition-transform',
                open && 'rotate-90',
              )}
            />
            进阶方式 · 手动 CLI
          </CollapsibleTrigger>
          <CollapsibleContent className="space-y-2 pt-2">
            <p className="text-xs text-muted-foreground">如果你希望自己掌控每一步，可以手动执行：</p>
            <CodeBlock code={advancedCli} />
            {advancedHint && (
              <p className="text-xs text-muted-foreground">{advancedHint}</p>
            )}
          </CollapsibleContent>
        </Collapsible>
      </div>
    </StepCard>
  )
}
