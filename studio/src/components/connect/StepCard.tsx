import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/Card'

interface StepCardProps {
  step: number
  title: string
  children: React.ReactNode
  optional?: boolean
}

export default function StepCard({ step, title, children, optional = false }: StepCardProps) {
  return (
    <Card>
      <div className="border-b border-border px-4 py-3 flex items-center gap-2">
        <span className="flex h-5 w-5 items-center justify-center rounded-full bg-primary/10 text-xs font-semibold text-primary">
          {step}
        </span>
        <h3 className="text-sm font-semibold text-foreground">{title}</h3>
        {optional && <Badge variant="secondary" className="text-[10px] px-1.5 py-0">可选</Badge>}
      </div>
      <CardContent className="space-y-3">
        {children}
      </CardContent>
    </Card>
  )
}
