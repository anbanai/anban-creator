import type { PortraitCandidate, PortraitChatMessage } from './portrait-chat-contract'

export interface PortraitTransport {
  capabilities(signal: AbortSignal): Promise<{ configured: boolean; speech_available: boolean; model?: string }>
  chat(messages: PortraitChatMessage[], signal: AbortSignal): Promise<{ candidate: PortraitCandidate }>
  transcribe?: (audio: Blob, signal: AbortSignal) => Promise<string>
}
