import type { LucideIcon } from 'lucide-react'
import { Badge } from '@/components/ui/Badge'

export interface Provider {
  id: string
  name: string
  icon: LucideIcon
  status: 'available' | 'coming-soon'
}

interface ProviderNavProps {
  providers: Provider[]
  activeId: string
  onSelect: (id: string) => void
}

export default function ProviderNav({ providers, activeId, onSelect }: ProviderNavProps) {
  return (
    <div className="md:w-48 shrink-0">
      {/* Mobile: horizontal scroll */}
      <div className="flex gap-2 overflow-x-auto pb-2 md:hidden">
        {providers.map((provider) => (
          <button
            key={provider.id}
            onClick={() => onSelect(provider.id)}
            disabled={provider.status === 'coming-soon'}
            aria-pressed={activeId === provider.id}
            title={provider.status === 'coming-soon' ? '即将支持' : undefined}
            className={`flex shrink-0 items-center gap-2 rounded-lg border px-3 py-2 text-sm font-medium transition-colors ${
              activeId === provider.id
                ? 'border-primary bg-primary/5 text-foreground'
                : 'border-border text-muted-foreground hover:border-primary/30 hover:bg-muted/50'
            } ${provider.status === 'coming-soon' ? 'opacity-60' : ''}`}
          >
            <provider.icon className="h-4 w-4" />
            {provider.name}
            {provider.status === 'coming-soon' && (
              <Badge variant="neutral" className="text-[10px] px-1.5 py-0">即将支持</Badge>
            )}
          </button>
        ))}
      </div>

      {/* Desktop: vertical list */}
      <nav className="hidden md:block space-y-1" aria-label="AI 提供商列表">
        {providers.map((provider) => (
          <button
            key={provider.id}
            onClick={() => onSelect(provider.id)}
            disabled={provider.status === 'coming-soon'}
            aria-pressed={activeId === provider.id}
            title={provider.status === 'coming-soon' ? '即将支持' : undefined}
            className={`flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm font-medium transition-colors ${
              activeId === provider.id
                ? 'border-l-2 border-primary bg-sidebar-accent text-sidebar-foreground -ml-[2px] pl-[calc(0.75rem+2px)]'
                : 'text-muted-foreground hover:bg-sidebar-accent hover:text-sidebar-foreground'
            } ${provider.status === 'coming-soon' ? 'opacity-60' : ''}`}
          >
            <provider.icon className="h-4 w-4 shrink-0" />
            <span className="flex-1 text-left">{provider.name}</span>
            {provider.status === 'coming-soon' && (
              <Badge variant="neutral" className="text-[10px] px-1.5 py-0">即将支持</Badge>
            )}
          </button>
        ))}
      </nav>
    </div>
  )
}
