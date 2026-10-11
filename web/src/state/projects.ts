import { useEffect, useState } from 'react'
import { projects } from '../api/client'
import type { ProjectDetail, ProjectList, ProjectWork } from '../api/types'
import { useSnapshot } from './context'
import type { Loaded } from './history'

/** How often a project view reloads when the feed is quiet: spend moves without an event. */
const REFRESH_MS = 30_000

/**
 * Loads a project view, again whenever the feed moves (a lane, run or decision changed)
 * and every half minute. The last answer stays on show while the next loads, and after a
 * failure.
 */
function useProjectLoad<T>(key: string, load: () => Promise<T>): Loaded<T> {
  const snap = useSnapshot()
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const [tick, setTick] = useState(0)
  const [state, setState] = useState<Loaded<T> & { key: string }>({ data: null, error: null, loading: true, key })

  useEffect(() => {
    const id = window.setInterval(() => setTick((n) => n + 1), REFRESH_MS)
    return () => window.clearInterval(id)
  }, [])

  useEffect(() => {
    let current = true
    load().then(
      (data) => current && setState({ data, error: null, loading: false, key }),
      (e: unknown) => current && setState((s) => ({ ...s, error: e instanceof Error ? e.message : String(e), loading: false, key })),
    )
    return () => {
      current = false
    }
    // load closes over what key names.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, feedLast, tick])

  if (state.key !== key) return { data: null, error: null, loading: true }
  return state
}

export const useProjectList = (): Loaded<ProjectList> => useProjectLoad('list', projects.list)

/** One project's detail; name null is the Unassigned group. */
export const useProject = (name: string | null): Loaded<ProjectDetail> =>
  useProjectLoad(name === null ? 'unassigned' : `project:${name}`, () => (name === null ? projects.unassigned() : projects.one(name)))

/** One project's work matrices and drift; name null is the Unassigned group. */
export const useProjectWork = (name: string | null): Loaded<ProjectWork> =>
  useProjectLoad(name === null ? 'work:unassigned' : `work:${name}`, () => projects.work(name))
