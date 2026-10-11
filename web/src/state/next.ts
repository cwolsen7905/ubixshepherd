import { useCallback, useEffect, useState } from 'react'
import { nextUp } from '../api/client'
import type { NextList } from '../api/types'

const POLL = 20_000

export type LoadNext = (all: boolean) => Promise<NextList>

export const loadNext: LoadNext = (all) => nextUp.list(all)

/**
 * The next-up list, read now and every 20 seconds while mounted. `reload` reads it again
 * at once (after the person's action). A failed read keeps the last list and says why.
 */
export function useNextList(all: boolean, load: LoadNext = loadNext) {
  const [list, setList] = useState<NextList | null>(null)
  const [error, setError] = useState('')
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let live = true
    const read = () =>
      load(all).then(
        (l) => {
          if (!live) return
          setList(l)
          setError('')
        },
        (e: unknown) => live && setError(e instanceof Error ? e.message : String(e)),
      )
    void read()
    const id = window.setInterval(() => void read(), POLL)
    return () => {
      live = false
      window.clearInterval(id)
    }
  }, [all, load, tick])
  const reload = useCallback(() => setTick((t) => t + 1), [])
  return { list, error, reload }
}
