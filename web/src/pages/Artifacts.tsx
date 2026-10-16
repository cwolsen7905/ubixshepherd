import { useEffect, useLayoutEffect, useMemo, useRef, useState, useSyncExternalStore } from 'react'
import { topics as topicsApi } from '../api/client'
import { storedArtifacts, intentTag, type StoredArtifact } from '../artifacts/api'
import { bytes } from '../artifacts/ArtifactViewer'
import { StoredArtifactView } from '../artifacts/StoredArtifact'
import { since } from '../format'
import { useNow } from '../hooks'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import { EmptyState, Skeleton } from '../ui'
import '../styles/pages/artifacts.css'

type Filters = { repo?: number; lane?: number; run?: number; topic?: number }

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

// Set when the person opened a file from this list, so the back arrow can step Back (the
// list then comes up as it was) instead of pushing another entry.
let fromList = false

/** Every artifact across lanes and runs, newest first, filtered by repo, lane, run and topic. */
export function ArtifactsPage({ repo = 0, lane = 0, run = 0, topic = 0 }: Filters) {
  const snap = useSnapshot()
  const now = useNow(30_000)
  const feedLast = snap.feed.at(-1)?.id ?? 0
  const [list, setList] = useState<{ key: string; rows?: StoredArtifact[]; error?: string } | null>(null)
  const [topicList, setTopicList] = useState<{ id: number; title: string }[]>([])
  const key = `${lane}:${run}:${topic}:${feedLast}`
  const hash = useSyncExternalStore(subscribe, () => window.location.hash)
  const viewing = viewedId(hash)
  const scrolled = useRef(0)
  const wasViewing = useRef(0)

  useLayoutEffect(() => {
    if (viewing && !wasViewing.current) window.scrollTo(0, 0)
    if (!viewing && wasViewing.current) window.scrollTo(0, scrolled.current)
    if (!viewing) fromList = false
    wasViewing.current = viewing
  }, [viewing])
  useEffect(() => () => void (fromList = false), [])
  const back = () => {
    if (fromList) {
      fromList = false
      window.history.back()
    } else go(withView(window.location.hash, 0))
  }
  useEffect(() => {
    if (!viewing) return
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !e.defaultPrevented && !document.querySelector('dialog[open], [role="dialog"]')) back()
    }
    window.addEventListener('keydown', onKey)
    return () => window.removeEventListener('keydown', onKey)
  }, [viewing])

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

  const nav = (o: Filters) => href.artifacts({ repo, lane, run, topic, ...o })
  const here = list?.key === key ? list : null
  // The repo filter has no query of its own: the lane's repo decides.
  const rows = (here?.rows ?? []).filter((a) => !repo || a.repo_id === repo)
  const filtered = repo > 0 || lane > 0 || run > 0 || topic > 0

  const shown = viewing ? (here?.rows ?? []).find((a) => a.id === viewing) : undefined
  const listHash = withView(hash, 0)

  return (
    <div className="page page-wide artifacts-page">
      {viewing > 0 && (
        <section className="artifact-full" aria-label="Artifact">
          <header className="page-head artifact-full-head">
            <a className="artifact-back" href={listHash} aria-label="Back to the artifact list" onClick={(e) => {
              if (fromList && !e.metaKey && !e.ctrlKey && !e.shiftKey) {
                e.preventDefault()
                back()
              }
            }}>
              <span aria-hidden="true">←</span> Artifacts
            </a>
            <h1>{shown?.title || shown?.path || `Artifact ${viewing}`}</h1>
          </header>
          <StoredArtifactView key={viewing} id={viewing} refreshKey={feedLast} />
        </section>
      )}
      <div hidden={viewing > 0}>
      <header className="page-head">
        <h1>Artifacts</h1>
      </header>

      <div className="filters">
        <label>
          <span className="faint">Repo</span>
          <select value={repo} onChange={(e) => go(nav({ repo: Number(e.target.value) || undefined, lane: undefined }))}>
            <option value={0}>All repos</option>
            {repos.map(([id, name]) => (
              <option key={id} value={id}>{name}</option>
            ))}
          </select>
        </label>
        <label>
          <span className="faint">Lane</span>
          <select value={lane} onChange={(e) => go(nav({ lane: Number(e.target.value) || undefined }))}>
            <option value={0}>All lanes</option>
            {lane > 0 && !laneById.has(lane) && <option value={lane}>lane {lane}</option>}
            {snap.lanes
              .filter((l) => !repo || l.repo_id === repo)
              .map((l) => (
                <option key={l.id} value={l.id}>{l.repo} / {l.name}</option>
              ))}
          </select>
        </label>
        <label>
          <span className="faint">Topic</span>
          <select value={topic} onChange={(e) => go(nav({ topic: Number(e.target.value) || undefined }))}>
            <option value={0}>All topics</option>
            {topic > 0 && !topicList.some((t) => t.id === topic) && <option value={topic}>topic {topic}</option>}
            {topicList.map((t) => (
              <option key={t.id} value={t.id}>{t.title}</option>
            ))}
          </select>
        </label>
        {run > 0 && (
          <span className="faint">
            Run {run}{' '}
            <a className="link-button" href={nav({ run: undefined })}>
              Any run
            </a>
          </span>
        )}
        {filtered && (
          <a className="link-button" href={href.artifacts()}>
            Clear filters
          </a>
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
                <tr key={a.id}>
                  <td>
                    <a
                      className="artifact-path"
                      href={withView(hash, a.id)}
                      onClick={() => {
                        scrolled.current = window.scrollY
                        fromList = true
                      }}
                    >
                      {a.title || a.path}
                    </a>
                    {a.title && <span className="faint"> {a.path}</span>}
                  </td>
                  <td>
                    <a href={href.lane(a.lane_id)}>{l ? <><span className="faint">{l.repo} /</span> {l.name}</> : `lane ${a.lane_id}`}</a>
                  </td>
                  <td>{a.run_id > 0 ? <a href={nav({ run: a.run_id })}>{a.run_id}</a> : <span className="faint">none</span>}</td>
                  <td><span className={`artifact-tag artifact-tag-${a.intent}`}>{intentTag(a)}</span></td>
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
