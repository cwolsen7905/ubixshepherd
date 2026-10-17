// Hash routes, so the app needs nothing from the server that serves it.
import { useEffect, useRef, useState, useSyncExternalStore } from 'react'

/** The time filter on the history views. */
export type Range = 'today' | 'week'

/** The usage page's range: three presets and a custom pair of days. */
export type UsageRange = 'today' | 'week' | 'month' | 'custom'

/** The timeline's range: since local midnight, the last 24 hours, or the last 7 days. */
export type TimelineRange = 'today' | '24h' | 'week'

/**
 * What the inspector shows: `open=decision:5`, `open=lane:4/changes`, `open=run:17/log`. A
 * trailing `/full` (`open=lane:4/changes/full`) is the same view across the whole width, with a
 * back arrow; `mr:N` is the merge request of lane N.
 */
export type Open = { kind: string; id: number; tab?: string; full?: boolean }

/** The kinds the inspector can hold. A bare unknown kind is ignored rather than shown. */
export const OPEN_KINDS = ['decision', 'question', 'request', 'lane', 'run', 'topic', 'artifact', 'mr'] as const

type Page =
  | { page: 'board'; range?: Range }
  | { page: 'runs'; range?: Range; agent?: string; lane?: number }
  | { page: 'inbox'; kind?: string }
  | { page: 'usage'; range?: UsageRange; from?: string; to?: string }
  | { page: 'timeline'; range?: TimelineRange; agent?: string }
  | { page: 'next'; focus?: number }
  | { page: 'artifacts'; repo?: number; lane?: number; run?: number; topic?: number }
  | { page: 'history'; kind?: string; project?: string; q?: string }
  | { page: 'files'; repo?: number; lane?: number; ref?: string }
  | { page: 'rules' }
  | { page: 'projects' }
  | { page: 'project'; name: string }
  | { page: 'unassigned' }
  | { page: 'chat' }
  | { page: 'status' }
  | { page: 'settings'; scope?: string }
  | { page: 'lane'; id: number }
  | { page: 'log'; run: number }
  | { page: 'kit' }
  | { page: 'missing'; path: string }

/** Every route can carry an open inspector item. */
export type Route = Page & { open?: Open }

/** Reads an `open=` value, or undefined when it is not `kind:id[/tab]` of a known kind. */
export function parseOpen(v: string | null | undefined): Open | undefined {
  const m = /^([a-z]+):(\d+)(?:\/(?!full(?:\/|$))([a-z0-9-]+))?(\/full)?$/.exec(v ?? '')
  if (!m || !(OPEN_KINDS as readonly string[]).includes(m[1] as string)) return undefined
  const id = Number(m[2])
  if (!Number.isSafeInteger(id) || id <= 0) return undefined
  return { kind: m[1] as string, id, ...(m[3] ? { tab: m[3] } : {}), ...(m[4] ? { full: true } : {}) }
}

export function formatOpen(o: Open): string {
  return `${o.kind}:${o.id}${o.tab ? `/${o.tab}` : ''}${o.full ? '/full' : ''}`
}

/** `hash` with its `open=` set, or removed when `o` is undefined. Other parameters are kept. */
export function withOpen(hash: string, o: Open | undefined): string {
  const bare = hash.replace(/^#/, '') || '/'
  const [path = '/', query = ''] = bare.split('?')
  const q = new URLSearchParams(query)
  if (o) q.set('open', formatOpen(o))
  else q.delete('open')
  return q.size ? `#${path}?${q}` : `#${path}`
}

/**
 * The hash the app shows for `hash`: the old Decisions page lives on as the Inbox filtered
 * to decisions, and `?d=` becomes an open inspector item.
 */
export function canonical(hash: string): string {
  const bare = hash.replace(/^#/, '')
  const [path = '', query = ''] = bare.split('?')
  if (path !== '/decisions') return hash
  const q = new URLSearchParams(query)
  const d = Number(q.get('d'))
  const out = new URLSearchParams({ kind: 'decision' })
  const open = q.get('open') ?? (d > 0 ? `decision:${d}` : '')
  if (open) out.set('open', open)
  return `#/inbox?${out}`
}

export function parse(hash: string): Route {
  hash = canonical(hash)
  const route = parsePage(hash)
  const open = parseOpen(new URLSearchParams(hash.split('?')[1] ?? '').get('open'))
  return open ? { ...route, open } : route
}

function parsePage(hash: string): Page {
  const [path = '/', query = ''] = (hash.replace(/^#/, '') || '/').split('?')
  const q = new URLSearchParams(query)
  const range = q.get('range') === 'week' ? 'week' : q.get('range') === 'today' ? 'today' : undefined
  if (path === '/') return { page: 'chat' }
  if (path === '/board') return range ? { page: 'board', range } : { page: 'board' }
  if (path === '/runs') {
    const lane = Number(q.get('lane'))
    return {
      page: 'runs',
      ...(range ? { range } : {}),
      ...(q.get('agent') ? { agent: q.get('agent') as string } : {}),
      ...(lane > 0 ? { lane } : {}),
    }
  }
  if (path === '/inbox') return q.get('kind') ? { page: 'inbox', kind: q.get('kind') as string } : { page: 'inbox' }
  if (path === '/usage') {
    const r = q.get('range')
    const usageRange = r === 'today' || r === 'week' || r === 'month' || r === 'custom' ? r : undefined
    const day = /^\d{4}-\d{2}-\d{2}$/
    const from = q.get('from') ?? ''
    const to = q.get('to') ?? ''
    return {
      page: 'usage',
      ...(usageRange ? { range: usageRange } : {}),
      ...(usageRange === 'custom' && day.test(from) ? { from } : {}),
      ...(usageRange === 'custom' && day.test(to) ? { to } : {}),
    }
  }
  if (path === '/timeline') {
    const r = q.get('range')
    const tr = r === 'today' || r === '24h' || r === 'week' ? r : undefined
    return { page: 'timeline', ...(tr ? { range: tr } : {}), ...(q.get('agent') ? { agent: q.get('agent') as string } : {}) }
  }
  let m: RegExpExecArray | null
  if (path === '/projects') return { page: 'projects' }
  if (path === '/unassigned') return { page: 'unassigned' }
  m = /^\/projects\/(.+)$/.exec(path)
  if (m) {
    try {
      return { page: 'project', name: decodeURIComponent(m[1] as string) }
    } catch {
      return { page: 'missing', path }
    }
  }
  if (path === '/next') {
    const item = Number(q.get('item'))
    return item > 0 ? { page: 'next', focus: item } : { page: 'next' }
  }
  if (path === '/artifacts') {
    const n = (k: string) => {
      const v = Number(q.get(k))
      return Number.isSafeInteger(v) && v > 0 ? v : 0
    }
    const [repo, lane, run, topic] = [n('repo'), n('lane'), n('run'), n('topic')]
    return {
      page: 'artifacts',
      ...(repo ? { repo } : {}),
      ...(lane ? { lane } : {}),
      ...(run ? { run } : {}),
      ...(topic ? { topic } : {}),
    }
  }
  if (path === '/history') {
    return {
      page: 'history',
      ...(q.get('kind') ? { kind: q.get('kind') as string } : {}),
      ...(q.get('project') ? { project: q.get('project') as string } : {}),
      ...(q.get('q') ? { q: q.get('q') as string } : {}),
    }
  }
  if (path === '/files') {
    const n = (k: string) => {
      const v = Number(q.get(k))
      return Number.isSafeInteger(v) && v > 0 ? v : 0
    }
    const [repo, lane] = [n('repo'), n('lane')]
    return {
      page: 'files',
      ...(repo ? { repo } : {}),
      ...(lane ? { lane } : {}),
      ...(q.get('ref') ? { ref: q.get('ref') as string } : {}),
    }
  }
  if (path === '/rules') return { page: 'rules' }
  if (path === '/chat') return { page: 'chat' }
  if (path === '/status') return { page: 'status' }
  if (path === '/settings') return q.get('scope') ? { page: 'settings', scope: q.get('scope') as string } : { page: 'settings' }
  m = /^\/lanes\/(\d+)$/.exec(path)
  if (m) return { page: 'lane', id: Number(m[1]) }
  m = /^\/runs\/(\d+)\/log$/.exec(path)
  if (m) return { page: 'log', run: Number(m[1]) }
  if (path === '/kit' && import.meta.env.DEV) return { page: 'kit' }
  return { page: 'missing', path }
}

export const href = {
  board: (range?: Range) => (range ? `#/board?range=${range}` : '#/board'),
  runs: (o: { range?: Range; agent?: string; lane?: number } = {}) => {
    const q = new URLSearchParams()
    if (o.range) q.set('range', o.range)
    if (o.agent) q.set('agent', o.agent)
    if (o.lane) q.set('lane', String(o.lane))
    return q.size ? `#/runs?${q}` : '#/runs'
  },
  inbox: (kind?: string) => (kind ? `#/inbox?kind=${encodeURIComponent(kind)}` : '#/inbox'),
  /** Old links keep working: `#/decisions` redirects to the Inbox. */
  decisions: (focus?: number) => (focus ? `#/decisions?d=${focus}` : '#/decisions'),
  usage: (o: { range?: UsageRange; from?: string; to?: string } = {}) => {
    const q = new URLSearchParams()
    if (o.range) q.set('range', o.range)
    if (o.range === 'custom') {
      if (o.from) q.set('from', o.from)
      if (o.to) q.set('to', o.to)
    }
    return q.size ? `#/usage?${q}` : '#/usage'
  },
  timeline: (o: { range?: TimelineRange; agent?: string } = {}) => {
    const q = new URLSearchParams()
    if (o.range) q.set('range', o.range)
    if (o.agent) q.set('agent', o.agent)
    return q.size ? `#/timeline?${q}` : '#/timeline'
  },
  artifacts: (o: { repo?: number; lane?: number; run?: number; topic?: number } = {}) => {
    const q = new URLSearchParams()
    for (const k of ['repo', 'lane', 'run', 'topic'] as const) if (o[k]) q.set(k, String(o[k]))
    return q.size ? `#/artifacts?${q}` : '#/artifacts'
  },
  history: (o: { kind?: string; project?: string; q?: string } = {}) => {
    const q = new URLSearchParams()
    for (const k of ['kind', 'project', 'q'] as const) if (o[k]) q.set(k, o[k] as string)
    return q.size ? `#/history?${q}` : '#/history'
  },
  /** A repo's files: `lane` is the lane whose worktree is read, `ref` a branch, tag or sha instead. */
  files: (o: { repo?: number; lane?: number; ref?: string } = {}) => {
    const q = new URLSearchParams()
    if (o.repo) q.set('repo', String(o.repo))
    if (o.lane) q.set('lane', String(o.lane))
    if (o.ref) q.set('ref', o.ref)
    return q.size ? `#/files?${q}` : '#/files'
  },
  rules: () => '#/rules',
  projects: () => '#/projects',
  project: (name: string) => `#/projects/${encodeURIComponent(name)}`,
  unassigned: () => '#/unassigned',
  next: (item?: number) => (item ? `#/next?item=${item}` : '#/next'),
  chat: () => '#/',
  status: () => '#/status',
  settings: (scope?: string) => (scope ? `#/settings?scope=${encodeURIComponent(scope)}` : '#/settings'),
  lane: (id: number) => `#/lanes/${id}`,
  log: (run: number) => `#/runs/${run}/log`,
}

function subscribe(fn: () => void) {
  window.addEventListener('hashchange', fn)
  return () => window.removeEventListener('hashchange', fn)
}

export function useRoute(): Route {
  const hash = useSyncExternalStore(subscribe, () => window.location.hash)
  // Replace, not push: Back must not return to the address that redirected.
  useEffect(() => {
    const to = canonical(hash)
    if (to !== hash) window.history.replaceState(null, '', to)
  }, [hash])
  return parse(hash)
}

export function go(to: string) {
  window.location.hash = to.replace(/^#/, '')
}

let refocusCount = 0
const refocusSubs = new Set<() => void>()

/**
 * Asks the page to show its focused item again. A link to the hash the person is already on
 * fires no hashchange, so a click on it calls this instead.
 */
export function refocus() {
  refocusCount++
  refocusSubs.forEach((fn) => fn())
}

/** How long a focused item stays marked. */
export const FOCUS_MS = 4000

/**
 * Scrolls the element for `id` to the middle of the view once per navigation, and returns the
 * id while it is marked (for FOCUS_MS) so the page can highlight it. `present` says the item
 * is on the page now, so a list that loads after the route changes still gets its focus; a
 * `refocus()` runs it again for the same id.
 */
export function useFocusTarget(id: number | undefined, present: boolean, selector: (id: number) => string): number | undefined {
  const pulse = useSyncExternalStore(
    (fn) => {
      refocusSubs.add(fn)
      return () => void refocusSubs.delete(fn)
    },
    () => refocusCount,
  )
  const [lit, setLit] = useState<number | undefined>()
  const done = useRef('')
  const timer = useRef<number | undefined>(undefined)
  useEffect(() => {
    if (!id) {
      done.current = ''
      return
    }
    const key = `${id}:${pulse}`
    if (!present || done.current === key) return
    const el = document.querySelector(selector(id))
    if (!el) return
    done.current = key
    el.scrollIntoView?.({ block: 'center' })
    window.clearTimeout(timer.current)
    // The pulse must show on the commit that found the element, so this is a deliberate one-shot set.
    // eslint-disable-next-line react-hooks/set-state-in-effect
    setLit(id)
    timer.current = window.setTimeout(() => setLit(undefined), FOCUS_MS)
  }, [id, present, pulse, selector])
  useEffect(() => () => window.clearTimeout(timer.current), [])
  return id && lit === id ? lit : undefined
}

/** The page the person was on before this one, and how far down it they had scrolled. */
export type Came = { hash: string; scroll: number }

let here = typeof window === 'undefined' ? '' : window.location.hash
let before: Came | undefined
const scrolls = new Map<string, number>()

if (typeof window !== 'undefined') {
  window.addEventListener('scroll', () => scrolls.set(here, window.scrollY), { passive: true })
  window.addEventListener('hashchange', () => {
    const now = window.location.hash
    if (now === here) return
    before = { hash: here, scroll: scrolls.get(here) ?? 0 }
    here = now
  })
}

/** The page this one was reached from within the app, or undefined when it was opened directly (or from nowhere a link can return to). */
export function cameFrom(): Came | undefined {
  if (!before || parse(before.hash).page === 'missing') return undefined
  return before
}
