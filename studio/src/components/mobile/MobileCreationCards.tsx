import { ArrowUpRight, FileText, Image, ShoppingBag, Clapperboard } from 'lucide-react'
import { Link } from 'react-router-dom'
import { mobileCreations } from '@/lib/mobile-creation'

const icons = { 'wechat-article': FileText, seednote: Image, 'video-edit': Clapperboard, ecommerce: ShoppingBag }
export function MobileCreationCards({ projectId }: { projectId?: string }) {
  return <div className="mobile-creation-grid">{mobileCreations.map(item => {
    const Icon = icons[item.id]
    return <Link key={item.id} to={`/mobile/create?type=${item.id}${projectId ? `&project_id=${encodeURIComponent(projectId)}` : ''}`} className={`mobile-creation-card mobile-tone-${item.tone}`}>
      <span className="mobile-creation-card-top"><span className="mobile-creation-icon"><Icon size={22} strokeWidth={1.6} /></span><span className="mobile-card-number">{item.mark}</span></span>
      <strong>{item.title}</strong><span className="mobile-card-subtitle">{item.subtitle}</span><ArrowUpRight className="mobile-card-arrow" size={17} />
    </Link>
  })}</div>
}
