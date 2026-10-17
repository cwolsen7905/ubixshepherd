import { createContext, useContext } from 'react'
import { BACKOFF } from '../state/live'

/** Whether the daemon answers, and when the next read is due. Composers read it: Send is off while offline, and drafts stay. */
export interface OfflineState {
  offline: boolean
  /** Whole seconds until the next try; null while online or when it cannot be known. */
  retryIn: number | null
}

export const ONLINE: OfflineState = { offline: false, retryIn: null }

export const OfflineContext = createContext<OfflineState>(ONLINE)

/** The daemon's reachability, from the shell. Outside a shell the daemon counts as online. */
export function useOffline(): OfflineState {
  return useContext(OfflineContext)
}

/**
 * Seconds until the next read while the daemon is down. The live feed retries after 1s, then
 * doubles to a 15s cap (BACKOFF); this walks that schedule from the moment it went down.
 */
export function retryIn(downSince: number | null, now: number): number | null {
  if (downSince === null) return null
  const since = Math.max(0, now - downSince)
  let at = 0
  for (let wait: number = BACKOFF.min; ; wait = Math.min(wait * 2, BACKOFF.max)) {
    at += wait
    if (at > since) return Math.max(1, Math.ceil((at - since) / 1000))
  }
}

/** "Offline, retrying in 8s". */
export function offlineWords(s: OfflineState): string {
  return s.retryIn === null ? 'Offline, retrying' : `Offline, retrying in ${s.retryIn}s`
}
