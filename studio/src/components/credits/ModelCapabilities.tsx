import { Check, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import { tierBenefits, modelCategories } from '@/lib/labels'
import { Badge } from '@/components/ui/badge'
import { Card, CardContent } from '@/components/ui/card'

export default function ModelCapabilities({ currentTier }: { currentTier: string }) {
  return (
    <Card>
      <CardContent>
        <div className="space-y-4">
          <div>
            <h3 className="text-sm font-semibold text-foreground">模型能力</h3>
            <p className="mt-0.5 text-xs text-muted-foreground">不同会员等级可使用的模型类别</p>
          </div>

          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            {tierBenefits.map((tier) => {
              const isCurrent = tier.key === currentTier
              return (
                <div
                  key={tier.key}
                  className={cn(
                    'rounded-lg border border-border p-3',
                    isCurrent && 'border-primary/40 bg-primary/5',
                  )}
                >
                  <div className="mb-3 flex items-center gap-1.5">
                    <span className="text-sm font-semibold text-foreground">{tier.name}</span>
                    {tier.recommended && <Badge variant="default">推荐</Badge>}
                    {isCurrent && <Badge variant="secondary">当前</Badge>}
                  </div>

                  <ul className="space-y-2">
                    {modelCategories.map((cat) => {
                      const ok = cat.available[tier.key]
                      return (
                        <li key={cat.key} className="flex items-center gap-2 text-sm">
                          {ok ? (
                            <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-primary/10 text-primary">
                              <Check className="h-3 w-3" aria-label="包含" />
                            </span>
                          ) : (
                            <span className="inline-flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-muted text-muted-foreground">
                              <Minus className="h-3 w-3" aria-label="不包含" />
                            </span>
                          )}
                          <span className={ok ? 'text-foreground' : 'text-muted-foreground'}>
                            {cat.label}
                          </span>
                        </li>
                      )
                    })}
                  </ul>
                </div>
              )
            })}
          </div>

          <ul className="space-y-1 text-xs text-muted-foreground">
            {modelCategories.map((cat) => (
              <li key={cat.key}>
                <span className="font-medium text-foreground">{cat.label}</span>
                <span className="mx-1.5">—</span>
                {cat.description}
              </li>
            ))}
          </ul>
        </div>
      </CardContent>
    </Card>
  )
}
