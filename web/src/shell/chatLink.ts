// The chat stream's connection, lifted out of the chat page so the rail's one status dot
// can read it. The open thread publishes here; nothing is published when no chat is open.
import { useSyncExternalStore } from 'react'
import type { Connection } from '../state/desk'

export interface ChatLink {
  connection: Connection
  /** While reconnecting: when the next attempt runs (ms since the epoch), else null. */
  retryAt: number | null
}

let current: ChatLink | null = null
const subs = new Set<() => void>()

/** Sets the open chat's connection, or null when the chat closes. */
export function publishChat(link: ChatLink | null) {
  if (current === link || (current && link && current.connection === link.connection && current.retryAt === link.retryAt)) return
  current = link
  subs.forEach((fn) => fn())
}

function subscribe(fn: () => void) {
  subs.add(fn)
  return () => void subs.delete(fn)
}

/** The open chat's connection, or null when no chat is open (then only the daemon link counts). */
export function useChatLink(): ChatLink | null {
  return useSyncExternalStore(subscribe, () => current)
}

/** The dot's tone: `up` both links good, `warn` the chat stream is catching up, `down` something needs the person, `pending` the daemon has not answered yet. */
export type Tone = 'up' | 'warn' | 'down' | 'pending'

/** The worse of the daemon link and the chat stream. */
export function worstTone(daemon: { error: string | null; ready: boolean }, chat: ChatLink | null): Tone {
  if (daemon.error !== null) return 'down'
  if (chat && (chat.connection === 'signed-out' || chat.connection === 'refused')) return 'down'
  if (!daemon.ready) return 'pending'
  if (chat && (chat.connection === 'connecting' || chat.connection === 'reconnecting')) return 'warn'
  return 'up'
}

/** What the chat stream is doing, in words; `null` when no chat is open. */
export function chatWords(chat: ChatLink | null): string {
  switch (chat?.connection) {
    case undefined: return 'not open'
    case 'live': return 'live'
    case 'connecting': return 'connecting'
    case 'reconnecting': return 'reconnecting'
    case 'signed-out': return 'signed out'
    case 'refused': return 'refused'
  }
}
