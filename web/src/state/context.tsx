import { createContext, useContext, useEffect, useSyncExternalStore, type ReactNode } from 'react'
import type { Waiting } from '../api/types'
import { Live, type Snapshot } from './live'

const LiveContext = createContext<Live | null>(null)

export function LiveProvider({ live, children }: { live: Live; children: ReactNode }) {
  useEffect(() => {
    live.start()
    const onVisible = () => {
      if (document.visibilityState === 'visible') live.wake()
    }
    const onOnline = () => live.wake()
    document.addEventListener('visibilitychange', onVisible)
    window.addEventListener('online', onOnline)
    return () => {
      document.removeEventListener('visibilitychange', onVisible)
      window.removeEventListener('online', onOnline)
      live.stop()
    }
  }, [live])
  return <LiveContext.Provider value={live}>{children}</LiveContext.Provider>
}

export function useLive(): Live {
  const live = useContext(LiveContext)
  if (!live) throw new Error('useLive outside LiveProvider')
  return live
}

export function useSnapshot(): Snapshot {
  const live = useLive()
  return useSyncExternalStore(live.subscribe, live.getSnapshot)
}

/** The one attention list, or null before it loads or outside a provider (a unit-tested piece). */
export function useAttention(): Waiting | null {
  const live = useContext(LiveContext)
  const subscribe = live ? live.subscribe : noSubscribe
  const read: () => Snapshot | null = live ? live.getSnapshot : noSnapshot
  const snap = useSyncExternalStore(subscribe, read)
  return snap ? snap.waiting : null
}

const noSubscribe = () => () => {}
const noSnapshot = () => null
