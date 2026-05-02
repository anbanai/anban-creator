import { Check, Minus } from 'lucide-react'
import { cn } from '@/lib/utils'
import {
  membershipComparisonGroups,
  tierBenefits,
  type MembershipComparisonValue,
} from '@/lib/labels'
import Badge from '@/components/ui/Badge'

function ComparisonValue({ value }: { value: MembershipComparisonValue }) {
  if (value === true) {
    return (
      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-primary/10 text-primary">
        <Check className="h-4 w-4" aria-label="包含" />
      </span>
    )
  }

  if (value === false) {
    return (
      <span className="inline-flex h-6 w-6 items-center justify-center rounded-full bg-muted text-muted-foreground">
        <Minus className="h-4 w-4" aria-label="不包含" />
      </span>
    )
  }

  return <span className="text-sm font-medium leading-5 text-foreground">{value}</span>
}

export default function MembershipComparison({ currentTier }: { currentTier: string }) {
  return (
    <div className="overflow-hidden rounded-xl border border-border bg-card">
      <div className="overflow-x-auto">
        <table aria-label="会员权益对比" className="w-full min-w-[760px] border-collapse text-sm">
          <thead>
            <tr className="border-b border-border bg-muted/30">
              <th className="sticky left-0 z-20 w-44 bg-muted/30 px-4 py-4 text-left font-medium text-muted-foreground">
                用户权益
              </th>
              {tierBenefits.map((tier) => {
                const isCurrent = tier.key === currentTier
                return (
                  <th
                    key={tier.key}
                    className={cn(
                      'w-48 px-4 py-4 text-left align-top',
                      isCurrent && 'bg-primary/5',
                    )}
                  >
                    <div className="flex flex-wrap items-center gap-2">
                      <span className="text-base font-semibold text-foreground">{tier.name}</span>
                      {tier.recommended && <Badge variant="info">推荐</Badge>}
                      {isCurrent && <Badge variant="success">当前</Badge>}
                    </div>
                    <p className="mt-1 text-xs font-normal text-muted-foreground">{tier.description}</p>
                  </th>
                )
              })}
            </tr>
          </thead>
          <tbody>
            {membershipComparisonGroups.map((group) => (
              <ComparisonGroup key={group.title} group={group} currentTier={currentTier} />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  )
}

function ComparisonGroup({
  group,
  currentTier,
}: {
  group: (typeof membershipComparisonGroups)[number]
  currentTier: string
}) {
  return (
    <>
      <tr className="border-b border-border bg-muted/20">
        <th
          scope="rowgroup"
          colSpan={tierBenefits.length + 1}
          className="px-4 py-2 text-left text-xs font-semibold text-muted-foreground"
        >
          {group.title}
        </th>
      </tr>
      {group.rows.map((row) => (
        <tr key={row.label} className="border-b border-border last:border-b-0">
          <th className="sticky left-0 z-10 bg-card px-4 py-3 text-left font-medium text-muted-foreground">
            {row.label}
          </th>
          {tierBenefits.map((tier) => (
            <td
              key={tier.key}
              className={cn(
                'px-4 py-3 align-middle',
                tier.key === currentTier && 'bg-primary/5',
              )}
            >
              <ComparisonValue value={row.values[tier.key]} />
            </td>
          ))}
        </tr>
      ))}
    </>
  )
}
