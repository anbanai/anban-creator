import { useEffect, useRef, useCallback } from 'react'

interface UseWebSocketOptions {
  url: string
  onMessage?: (data: any) => void
  onOpen?: () => void
  onClose?: () => void
  onError?: (error: Event) => void
  enabled?: boolean
}

const MAX_RETRY_ATTEMPTS = 10
const INITIAL_DELAY = 1000
const MAX_DELAY = 30000

export function useWebSocket({ url, onMessage, onOpen, onClose, onError, enabled = true }: UseWebSocketOptions) {
  const wsRef = useRef<WebSocket | null>(null)
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | undefined>(undefined)
  const retryCountRef = useRef(0)
  const enabledRef = useRef(enabled)
  enabledRef.current = enabled

  // Use refs for all callbacks to prevent reconnection loops
  const onMessageRef = useRef(onMessage)
  onMessageRef.current = onMessage
  const onOpenRef = useRef(onOpen)
  onOpenRef.current = onOpen
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose
  const onErrorRef = useRef(onError)
  onErrorRef.current = onError

  const getBackoffDelay = useCallback(() => {
    const delay = Math.min(INITIAL_DELAY * Math.pow(2, retryCountRef.current), MAX_DELAY)
    return delay
  }, [])

  const connect = useCallback(() => {
    if (!enabledRef.current || !url) return

    if (retryCountRef.current >= MAX_RETRY_ATTEMPTS) {
      return
    }

    try {
      const ws = new WebSocket(url)
      wsRef.current = ws

      ws.onopen = () => {
        retryCountRef.current = 0 // Reset on successful connection
        onOpenRef.current?.()
      }

      ws.onmessage = (event) => {
        try {
          const data = JSON.parse(event.data)
          onMessageRef.current?.(data)
        } catch {
          // Non-JSON message, ignore
        }
      }

      ws.onclose = () => {
        onCloseRef.current?.()
        // Reconnect with exponential backoff
        const delay = getBackoffDelay()
        retryCountRef.current += 1
        if (retryCountRef.current < MAX_RETRY_ATTEMPTS) {
          reconnectTimeoutRef.current = setTimeout(connect, delay)
        }
      }

      ws.onerror = (error) => {
        onErrorRef.current?.(error)
      }
    } catch {
      // Connection failed, will retry
      const delay = getBackoffDelay()
      retryCountRef.current += 1
      if (retryCountRef.current < MAX_RETRY_ATTEMPTS) {
        reconnectTimeoutRef.current = setTimeout(connect, delay)
      }
    }
  }, [url, getBackoffDelay])

  useEffect(() => {
    retryCountRef.current = 0
    connect()

    return () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.onclose = null // Prevent reconnect
        wsRef.current.close()
      }
    }
  }, [connect])

  return {
    disconnect: () => {
      if (reconnectTimeoutRef.current) {
        clearTimeout(reconnectTimeoutRef.current)
      }
      if (wsRef.current) {
        wsRef.current.onclose = null
        wsRef.current.close()
      }
    },
  }
}
