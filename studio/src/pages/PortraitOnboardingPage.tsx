import { useState } from 'react'
import { Link } from 'react-router-dom'
import { useAuth } from '@/contexts/AuthContext'
import PortraitOnboarding from '@/components/projects/PortraitOnboarding'
import { onboardingApi } from '@/lib/api/onboarding'
import { portraitConversationSchema, validatePortraitCandidate, type PortraitCandidate, type PortraitChatMessage } from '@/lib/portrait-chat-contract'

type Session = { messages: PortraitChatMessage[]; candidate: PortraitCandidate }
type Draft = { id: string; session?: Session }

// A per-account, per-tab recovery draft only. Confirmed projects remain on Server.
function AccountInterview({ userId }: { userId: string }) {
  const storageKey = `anban:interview-draft:${userId}`
  const [warning, setWarning] = useState('')
  const [draft, setDraft] = useState<Draft>(() => {
    try {
      const raw = sessionStorage.getItem(storageKey)
      if (raw) {
        const saved = JSON.parse(raw)
        if (typeof saved.id === 'string' && /^[0-9a-f-]{36}$/.test(saved.id)) {
          if (!saved.session) return { id: saved.id }
          const messages = portraitConversationSchema.parse(saved.session.messages)
          return { id: saved.id, session: { messages, candidate: validatePortraitCandidate(saved.session.candidate, messages) } }
        }
      }
    } catch { /* A corrupt draft cannot become a confirmed project. */ }
    return { id: crypto.randomUUID() }
  })
  function persist(next: Draft) {
    try { sessionStorage.setItem(storageKey, JSON.stringify(next)); setWarning('') }
    catch { setWarning('浏览器无法保存临时进度，请先在创作页保存当前画像后再离开。') }
  }
  function reset() {
    const next = { id: crypto.randomUUID() }
    persist(next); setDraft(next)
  }
  return <>
    <Link to="/projects" className="text-sm text-muted-foreground">返回项目</Link>
    {warning && <p role="alert">{warning}</p>}
    <PortraitOnboarding key={draft.id} transport={onboardingApi} production onlineDelivery sessionId={draft.id}
      initialSession={draft.session} onReset={reset}
      onSessionChange={session => persist({ id: draft.id, session })} />
  </>
}

export default function PortraitOnboardingPage() {
  const { user } = useAuth()
  return user ? <AccountInterview key={user.id} userId={user.id}/> : null
}
