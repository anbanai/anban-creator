import { useLocation } from 'react-router-dom'
import { useIsMobile } from '@/hooks/use-mobile'
import AssistantWorkspace from './AssistantWorkspace'
import MobileStudioPage from './MobileStudioPage'

export default function StudioEntryPage() {
  const { state } = useLocation()
  const mobile = useIsMobile()
  const context = state as Record<string, unknown> | null
  const hasConversationContext = Boolean(context && ['modeId', 'projectId', 'contentId', 'checkpointId', 'initialMessage'].some(key => context[key]))
  return mobile && !hasConversationContext ? <MobileStudioPage /> : <AssistantWorkspace />
}
