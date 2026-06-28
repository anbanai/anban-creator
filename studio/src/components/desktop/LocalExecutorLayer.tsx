import { useEffect } from 'react'
import { toast } from 'sonner'
import { isDesktop, onLocalRunEvent, getLocalExecutorStatus } from '@/lib/tauri'
import {
  localExecutorStore,
  useLocalExecutorStatus,
  useLocalExecutorWizardOpen,
  useLocalExecutorWizardDismissed,
} from '@/lib/local-executor-store'
import FirstRunWizard from './FirstRunWizard'
import LocalRunLogSheet from './LocalRunLogSheet'

// Dedupe identical consecutive error toasts so a flapping executor (e.g. auth
// failing every claim cycle) doesn't flood the user. The line still lands in
// the live-log drawer on every occurrence; only the toast is suppressed.
let lastErrorToast = ''

/**
 * Root desktop orchestrator, mounted once inside the authenticated layout.
 * It is the single place that:
 *   1. subscribes to the `local-run://event` stream (previously emitted by the
 *      Rust executor but never consumed) and feeds it into the store + toasts,
 *   2. polls provisioning status into the store (so the sidebar pill and
 *      drawer stay live without each polling themselves),
 *   3. auto-opens the first-run wizard when the machine isn't provisioned.
 *
 * In a browser every IPC call is a no-op, so this layer is inert — it only
 * renders the two store-controlled overlays, which stay closed.
 */
export default function LocalExecutorLayer() {
  const status = useLocalExecutorStatus()
  const wizardOpen = useLocalExecutorWizardOpen()
  const wizardDismissed = useLocalExecutorWizardDismissed()

  // (1) Subscribe to the local-run event stream.
  useEffect(() => {
    if (!isDesktop()) return
    let unlisten: (() => void) | null = null
    let cancelled = false
    void (async () => {
      unlisten = await onLocalRunEvent((e) => {
        localExecutorStore.pushLog(e)
        if (e.level === 'error') {
          if (e.message !== lastErrorToast) {
            lastErrorToast = e.message
            toast.error(e.message)
          }
        } else if (e.message.startsWith('已认领任务')) {
          toast(e.message)
        }
      })
      if (cancelled && unlisten) unlisten()
    })()
    return () => {
      cancelled = true
      if (unlisten) unlisten()
    }
  }, [])

  // (2) Poll provisioning status into the store.
  useEffect(() => {
    if (!isDesktop()) return
    let cancelled = false
    const tick = async () => {
      const s = await getLocalExecutorStatus()
      if (!cancelled) localExecutorStore.setStatus(s)
    }
    void tick()
    const id = window.setInterval(tick, 4000)
    return () => {
      cancelled = true
      window.clearInterval(id)
    }
  }, [])

  // (3) Auto-open the wizard once per session when not provisioned.
  useEffect(() => {
    if (!isDesktop()) return
    if (status && !status.available && !wizardDismissed && !wizardOpen) {
      localExecutorStore.openWizard()
    }
  }, [status?.available, wizardDismissed, wizardOpen])

  return (
    <>
      <FirstRunWizard />
      <LocalRunLogSheet />
    </>
  )
}
