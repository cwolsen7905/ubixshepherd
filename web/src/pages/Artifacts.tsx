import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { topics as topicsApi } from '../api/client'
import { StoredArtifactView } from '../artifacts/StoredArtifact'
import { intentTag, intentTone, storedArtifacts, type Intent, type StoredArtifact } from '../artifacts/api'
import { bytes } from '../artifacts/ArtifactViewer'
import { since } from '../format'
import { useNow } from '../hooks'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import { EmptyState, Skeleton } from '../ui'
import './Artifacts.css'

type Filters = { repo?: number; lane?: number; run?: number; topic?: number; intent?: Intent | 'all' }

const INTENTS: { value: Intent | 'all'; label: string }[] = [
  { value: 'all', label: 'All intents' },
  { value: 'commit', label: 'commit' },
  { value: 'keep', label: 'kept local' },
  { value: 'scratch', label: 'scratch' },
  { value: 'undecided', label: 'undecided' },
]

// The open file rides in the address as `view=ID`, a parameter of this page alone, so a
// link to it works and Back returns to the list.
const subscribe = (fn: () => void) => {
  window.addEventListener('hashchange', fn)
  return () => window.removeEventListener('hashchange', fn)
}

/** The artifact id in the address's `view=`, or 0. */
export function viewedId(hash: string): number {
  const v = Number(new URLSearchParams(hash.split('?')[1] ?? '').get('view'))
  return Number.isSafeInteger(v) && v > 0 ? v : 0
}

/** `hash` with `view=` set to `id`, or removed when 0. Other parameters are kept. */
export function withView(hash: string, id: number): string {
  const [path = '/artifacts', query = ''] = hash.replace(/^#/, '').split('?')
  const q = new URLSearchParams(query)
  if (id) q.set('view', String(id))
  else q.delete('view')
  return q.size ? `#${path}?${q}` : `#${path}`
}

/** The intent in the address's `intent=`, or `all`. Like `view=`, a parameter of this page alone. */
export function intentOf(hash: string): Intent | 'all' {
  const v = new URLSearchParams(hash.split('?')[1] ?? '').get('intent')
  return INTENTS.some((i) => i.value === v) ? (v as Intent) : 'all'
}

/** `hash` with `intent=` set, or removed for `all`. Other parameters are kept. */
export function withIntent(hash: string, intent: Intent | 'all'): string {
  const [path = '/artifacts', query = ''] = hash.replace(/^#/, '').split('?')
  const q = new URLSearchParams(query)
  if (intent !== 'all') q.set('intent', intent)
  else q.delete('intent')
  return q.size ? `#${path}?${q}` : `#${path}`
}

// Set when the person opened a file from this list, so the back arrow can step Back (the
// list then comes up as it was) instead of pushing another entry.
let fromList = false

/** Whether a file's kind makes its path read as code. */
function isCode(a: StoredArtifact): boolean {
  return a.kind === 'code'
}

/** Every artifact across lanes and runs, newest first, filtered by repo, lane, run, topic and intent. */
export function ArtifactsPage({ repo = 0, lane = 0, run = 0, topic = 0 }: Filters) {
  const snap = useSnapshot()
  const now = useNow(30_000)
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const [list, setList] = useState<{ key: string; rows?: StoredArtifact[]; error?: string } | null>(null)
  const [topicList, setTopicList] = useState<{ id: number; title: string }[]>([])
  const key = `${lane}:${run}:${topic}:${feedLast}`
  const hash = useSyncExternalStore(subscribe, () => window.location.hash)
  const viewing = viewedId(hash)
  const intent = intentOf(hash)
  const scrolled = useRef(0)

  useLayoutEffect(() => {
    if (viewing && !fromList) window.scrollTo(0, 0)
    if (!viewing && scrolled.current) window.scrollTo(0, scrolled.current)
    if (!viewing) scrolled.current = 0
    if (!viewing) fromList = false
  }, [viewing])
  useEffect(() => () => void (fromList = false), [])

  const open = (id: number) => {
    scrolled.current = window.scrollY
    fromList = true
    go(withView(hash, id))
  }

  const stepBack = () => {
    fromList = false
    window.history.back()
  }

  useEffect(() => {
    if (!viewing) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('dialog[open], [role="dialog"]')) {
        if (fromList) {
          stepBack()
        } else {
          go(withView(hash, 0))
        }
      }
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [viewing, hash])

  useEffect(() => {
    let current = true
    storedArtifacts.list({ lane_id: lane, run_id: run, topic_id: topic }).then(
      (rows) => current && setList({ key, rows }),
      (e: unknown) => current && setList({ key, error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      current = false
    }
  }, [lane, run, topic, key])
  useEffect(() => {
    let current = true
    topicsApi.list().then(
      (t) => current && setTopicList(t.topics.map((x) => ({ id: x.id, title: x.title }))),
      () => undefined,
    )
    return () => {
      current = false
    }
  }, [])

  const laneById = useMemo(() => new Map(snap.lanes.map((l) => [l.id, l])), [snap.lanes])
  const repos = useMemo(() => {
    const m = new Map<number, string>()
    for (const l of snap.lanes) m.set(l.repo_id, l.repo)
    return [...m].sort((a, b) => a[1].localeCompare(b[1]))
  }, [snap.lanes])

  const lanesForRepo = useMemo(() => snap.lanes.filter((l) => !repo || l.repo_id === repo).sort((a, b) => a.name.localeCompare(b.name)), [snap.lanes, repo])

  const nav = (o: Filters) => withIntent(href.artifacts({ repo, lane, run, topic, ...o }), intent)
  const here = list?.key === key ? list : null
  // The repo filter has no query of its own: the lane's repo decides.
  const allRows = (here?.rows ?? []).filter((a) => !repo || a.repo_id === repo)
  const rows = allRows.filter((a) => intent === 'all' || a.intent === intent)
  const filtered = repo > 0 || lane > 0 || run > 0 || topic > 0 || intent !== 'all'

  const listHash = withView(hash, 0)

  if (viewing > 0) {
    return (
      <div className="sh-centre artifacts-page">
        <div className="sh-scroll">
          <StoredArtifactView
            key={viewing}
            id={viewing}
            refreshKey={feedLast}
            layout="page"
            backHref={listHash}
            onBack={fromList ? stepBack : undefined}
          />
        </div>
      </div>
    )
  }

  return (
    <div className="sh-centre artifacts-page">
      <header className="sh-head">
        <h1>Artifacts</h1>
        {filtered && (
          <a className="sh-head-right sh-link" href={href.artifacts()} onClick={(e) => { e.preventDefault(); go(href.artifacts()) }}>
            Clear filters
          </a>
        )}
      </header>
      <div className="sh-scroll">
        <div className="artifact-filters">
          <label className="artifact-filter">
            <span>Repo</span>
            <select
              className="sh-input"
              value={repo || ''}
              onChange={(e) => go(nav({ repo: Number(e.target.value) || undefined, lane: undefined }))}
            >
              <option value="">All repos</option>
              {repos.map(([id, name]) => (
                <option key={id} value={id}>{name}</option>
              ))}
            </select>
          </label>
          <label className="artifact-filter">
            <span>Lane</span>
            <select
              className="sh-input"
              value={lane || ''}
              onChange={(e) => go(nav({ lane: Number(e.target.value) || undefined }))}
            >
              <option value="">All lanes</option>
              {lane > 0 && !laneById.has(lane) && <option value={lane}>lane {lane}</option>}
              {lanesForRepo.map((l) => (
                <option key={l.id} value={l.id}>{l.repo} / {l.name}</option>
              ))}
            </select>
          </label>
          <label className="artifact-filter">
            <span>Topic</span>
            <select
              className="sh-input"
              value={topic || ''}
              onChange={(e) => go(nav({ topic: Number(e.target.value) || undefined }))}
            >
              <option value="">All topics</option>
              {topic > 0 && !topicList.some((t) => t.id === topic) && <option value={topic}>topic {topic}</option>}
              {topicList.map((t) => (
                <option key={t.id} value={t.id}>{t.title}</option>
              ))}
            </select>
          </label>
          <label className="artifact-filter">
            <span>Intent</span>
            <select
              className="sh-input"
              value={intent}
              onChange={(e) => go(withIntent(hash, e.target.value as Intent | 'all'))}
            >
              {INTENTS.map((i) => (
                <option key={i.value} value={i.value}>{i.label}</option>
              ))}
            </select>
          </label>
          {run > 0 && (
            <span className="artifact-run-chip">
              Run {run}{' '}
              <a className="sh-link" href={nav({ run: undefined })} onClick={(e) => { e.preventDefault(); go(nav({ run: undefined })) }}>
                Any run
              </a>
            </span>
          )}
        </div>

        {here?.error && <p className="tone-bad">Could not load artifacts: {here.error}</p>}
        {!here && <Skeleton lines={4} />}
        {here?.rows && rows.length === 0 && (
          <EmptyState title={filtered ? 'No artifact matches these filters' : 'No artifacts yet'}>
            {filtered ? 'Try clearing a filter.' : 'A file an agent declares, or one that matches the repo’s artifact globs, appears here.'}
          </EmptyState>
        )}
        {rows.length > 0 && (
          <table className="artifact-table">
            <thead>
              <tr>
                <th scope="col">File</th>
                <th scope="col">Lane</th>
                <th scope="col">Run</th>
                <th scope="col">Intent</th>
                <th scope="col" className="num">Size</th>
                <th scope="col">Updated</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((a) => {
                const l = laneById.get(a.lane_id)
                return (
                  <tr
                    key={a.id}
                    className="artifact-row"
                    tabIndex={0}
                    onClick={(e) => {
                      if (e.metaKey || e.ctrlKey || e.shiftKey) return
                      open(a.id)
                    }}
                    onKeyDown={(e) => {
                      // A link inside the row handles its own keys.
                      if (e.target !== e.currentTarget) return
                      if (e.key === 'Enter' || e.key === ' ') {
                        e.preventDefault()
                        open(a.id)
                      }
                    }}
                  >
                    <td>
                      <a
                        className="artifact-title"
                        href={withView(hash, a.id)}
                        onClick={(e) => {
                          // A modified click opens a new tab; a plain one is the row's.
                          if (e.metaKey || e.ctrlKey || e.shiftKey) e.stopPropagation()
                          else e.preventDefault()
                        }}
                      >
                        {a.title || a.path}
                      </a>
                      {a.title && <span className={isCode(a) ? 'artifact-subpath-mono' : 'artifact-subpath'}> {a.path}</span>}
                    </td>
                    <td>
                      <a className="sh-link" href={href.lane(a.lane_id)} onClick={(e) => e.stopPropagation()}>
                        {l ? <><span className="artifact-repo">{l.repo}</span> / {l.name}</> : `lane ${a.lane_id}`}
                      </a>
                    </td>
                    <td>{a.run_id > 0 ? <a className="sh-link" href={nav({ run: a.run_id })} onClick={(e) => e.stopPropagation()}>{a.run_id}</a> : <span className="faint">none</span>}</td>
                    <td><span className={`sh-tag${intentTone(a) === 'plain' ? '' : ` sh-tag--${intentTone(a)}`}`}>{intentTag(a)}</span></td>
                    <td className="num faint">{bytes(a.size)}</td>
                    <td className="faint" title={a.updated}>{since(a.updated, now) || 'now'}</td>
                  </tr>
                )
              })}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
