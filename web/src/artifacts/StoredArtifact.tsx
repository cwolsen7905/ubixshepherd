import { useEffect, useMemo, useState } from 'react'
import { ApiError } from '../api/client'
import { copyText } from '../components/CopyButton'
import { Markdown } from '../conversation/Markdown'
import { since } from '../format'
import { useNow } from '../hooks'
import { href, withOpen } from '../router'
import { Button, Dialog, EmptyState, Skeleton } from '../ui'
import { bytes } from './ArtifactViewer'
import { askToCommit, intentTag, storedArtifacts, storedContentPath, storedText, type StoredArtifact } from './api'
import { diffLines } from './diff'
import '../styles/pages/artifacts.css'

/** Reads one stored artifact, again when `refreshKey` changes. */
export function useStoredArtifact(id: number, refreshKey = 0) {
  const [state, setState] = useState<{ id: number; value?: StoredArtifact; error?: string; tick: number }>({ id, tick: 0 })
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let current = true
    storedArtifacts.one(id).then(
      (value) => current && setState({ id, value, tick }),
      (e: unknown) => current && setState({ id, error: e instanceof Error ? e.message : String(e), tick }),
    )
    return () => {
      current = false
    }
  }, [id, refreshKey, tick])
  const mine = state.id === id ? state : null
  return { artifact: mine?.value ?? null, error: mine?.error ?? null, reload: () => setTick((n) => n + 1) }
}

const textual = (a: StoredArtifact) => a.kind === 'markdown' || a.kind === 'text' || a.kind === 'code'

/** The version of a sha, newest first as the daemon lists them. */
function versionLabel(i: number, count: number): string {
  return i === 0 ? 'latest' : `v${count - i}`
}

/**
 * One stored artifact: its bar, the choice of what to do with it, its versions, and its
 * content: rendered, as source, or as a comparison of an older version with the latest.
 * Content is untrusted and never becomes markup.
 */
export function StoredArtifactView({ id, refreshKey, onChanged }: { id: number; refreshKey?: number; onChanged?: () => void }) {
  const { artifact, error, reload } = useStoredArtifact(id, refreshKey)
  const now = useNow(30_000)
  const [version, setVersion] = useState('')
  const [mode, setMode] = useState<'view' | 'source' | 'compare'>('view')
  const [confirming, setConfirming] = useState(false)
  const [busy, setBusy] = useState(false)
  const [problem, setProblem] = useState<string | null>(null)
  const [note, setNote] = useState<string | null>(null)
  // Set as soon as the desk takes the turn, so a retry of a failed patch cannot ask twice.
  const [asked, setAsked] = useState(false)

  const versions = useMemo(
    () => [...(artifact?.versions ?? [])].sort((a, b) => Date.parse(b.seen) - Date.parse(a.seen)),
    [artifact?.versions],
  )
  const newest = versions[0]?.sha256 ?? ''
  const older = version !== '' && version !== newest
  // A version the artifact no longer lists falls back to the newest.
  const chosen = versions.some((v) => v.sha256 === version) ? version : ''
  const shown = chosen === newest ? '' : chosen
  const effectiveMode = mode === 'compare' && !older ? 'view' : mode

  if (error && !artifact) {
    return (
      <div className="inspector-body">
        <p className="tone-bad">Could not read artifact {id}: {error}</p>
        <button type="button" className="link-button" onClick={reload}>
          Retry
        </button>
      </div>
    )
  }
  if (!artifact) return <Skeleton lines={4} />

  const a = artifact
  const live = a.state === 'live'
  const act = async (what: () => Promise<unknown>, done?: string) => {
    setBusy(true)
    setProblem(null)
    setNote(null)
    try {
      await what()
      if (done) setNote(done)
      reload()
      onChanged?.()
    } catch (e) {
      setProblem(e instanceof ApiError || e instanceof Error ? e.message : String(e))
    } finally {
      setBusy(false)
    }
  }
  return (
    <div className="stored-artifact">
      <p className="artifact-head">
        <code>{a.path}</code> <span className={`artifact-tag artifact-tag-${a.intent}`}>{intentTag(a)}</span>
      </p>
      <p className="artifact-bar faint">
        <a href={withOpen(window.location.hash, { kind: 'run', id: a.run_id })}>run {a.run_id}</a>
        {' · '}
        {a.kind} · {bytes(a.size)} · {since(a.updated, now) || 'now'} ago ·{' '}
        <a href={href.lane(a.lane_id)}>lane {a.lane_id}</a>
      </p>

      <div className="artifact-actions" role="group" aria-label="What to do with this file">
        {textual(a) && (
          <>
            <Button variant="ghost" aria-pressed={effectiveMode === 'source'} onClick={() => setMode(effectiveMode === 'source' ? 'view' : 'source')}>
              Source
            </Button>
            <Button
              variant="ghost"
              onClick={() =>
                void storedText(a.id, shown)
                  .then((t) => copyText(t.text))
                  .then((ok) => setNote(ok ? 'Copied.' : 'Could not copy.'), (e: unknown) => setProblem(e instanceof Error ? e.message : String(e)))
              }
            >
              Copy
            </Button>
          </>
        )}
        <a className="link-button" href={storedContentPath(a.id, shown)} target="_blank" rel="noreferrer">
          Open raw
        </a>
        <Button variant="ghost" disabled={busy} onClick={() => void act(() => storedArtifacts.patch(a.id, { pinned: !a.pinned }))}>
          {a.pinned ? 'Unpin' : 'Pin'}
        </Button>
        {live && (
          <>
            <Button disabled={busy || a.intent === 'keep'} onClick={() => void act(() => storedArtifacts.patch(a.id, { intent: 'keep' }), 'Kept by Shepherd, not for the repo.')}>
              Keep
            </Button>
            <Button
              disabled={busy || (asked && a.intent === 'commit')}
              onClick={() =>
                void act(async () => {
                  if (!asked) {
                    await askToCommit(a)
                    setAsked(true)
                  }
                  await storedArtifacts.patch(a.id, { intent: 'commit' })
                }, 'Asked the front desk to have the lane’s agent commit it.')
              }
            >
              {asked ? 'Asked to commit' : 'Ask to commit'}
            </Button>
          </>
        )}
        {(live || a.state === 'kept') && (
          <Button variant="danger" disabled={busy} onClick={() => setConfirming(true)}>
            Discard…
          </Button>
        )}
      </div>
      {problem && <p className="tone-bad" role="alert">{problem}</p>}
      {note && <p className="faint" role="status">{note}</p>}

      {versions.length > 1 && (
        <div className="artifact-versions">
          <h3>Versions</h3>
          <ul>
            {versions.map((v, i) => (
              <li key={v.sha256}>
                <button
                  type="button"
                  className="link-button"
                  aria-current={(chosen || newest) === v.sha256}
                  onClick={() => {
                    setVersion(v.sha256)
                    if (v.sha256 === newest) setMode('view')
                  }}
                >
                  {versionLabel(i, versions.length)}
                </button>{' '}
                <span className="faint">
                  {v.run_id > 0 ? `run ${v.run_id}` : 'changed outside a run'} · {since(v.seen, now) || 'now'} ago · {bytes(v.size)}
                </span>
              </li>
            ))}
          </ul>
          {older && textual(a) && (
            <Button variant="ghost" aria-pressed={effectiveMode === 'compare'} onClick={() => setMode(effectiveMode === 'compare' ? 'view' : 'compare')}>
              {effectiveMode === 'compare' ? 'Show this version' : 'Compare with latest'}
            </Button>
          )}
        </div>
      )}

      <Body a={a} version={shown} mode={effectiveMode} newest={newest} refreshKey={refreshKey} />

      <Dialog
        open={confirming}
        onClose={() => setConfirming(false)}
        title="Discard this file?"
        actions={
          <>
            <Button variant="ghost" onClick={() => setConfirming(false)}>
              Keep it
            </Button>
            <Button
              variant="danger"
              disabled={busy}
              onClick={() => {
                setConfirming(false)
                void act(() => storedArtifacts.discard(a.id))
              }}
            >
              Discard
            </Button>
          </>
        }
      >
        <p>
          <code>{a.path}</code> is deleted from the lane’s worktree and from Shepherd’s snapshots. This cannot be undone.
        </p>
      </Dialog>
    </div>
  )
}

function Body({ a, version, mode, newest, refreshKey }: { a: StoredArtifact; version: string; mode: 'view' | 'source' | 'compare'; newest: string; refreshKey?: number }) {
  const key = `${a.id}:${version}:${mode}:${a.sha256 ?? ''}:${refreshKey ?? 0}`
  const wantsText = textual(a) && a.state !== 'discarded'
  const [read, setRead] = useState<{ key: string; text?: string; masked?: number; latest?: string; error?: string } | null>(null)
  useEffect(() => {
    if (!wantsText) return
    let current = true
    const both = mode === 'compare' ? storedText(a.id, newest).then((t) => t.text) : Promise.resolve(undefined)
    Promise.all([storedText(a.id, version), both]).then(
      ([t, latest]) => current && setRead({ key, text: t.text, masked: t.redacted, ...(latest !== undefined ? { latest } : {}) }),
      (e: unknown) => current && setRead({ key, error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      current = false
    }
  }, [a.id, version, mode, newest, key, wantsText])

  if (a.state === 'discarded') return <EmptyState title="Discarded">This file was deleted at the person’s word.</EmptyState>
  if (a.kind === 'image') return <img className="artifact-image" src={storedContentPath(a.id, version)} alt={a.path} />
  if (!wantsText) return <EmptyState title="No preview">There is no preview for this kind of file.</EmptyState>
  const mine = read?.key === key ? read : null
  if (!mine) return <Skeleton lines={3} />
  if (mine.error) return <EmptyState title="Could not read the file">{mine.error}</EmptyState>
  const masked = (mine.masked ?? 0) > 0 && (
    <p className="faint">{mine.masked} secret-like {mine.masked === 1 ? 'span was' : 'spans were'} masked.</p>
  )
  if (mode === 'compare') {
    const lines = diffLines(mine.text ?? '', mine.latest ?? '')
    if (!lines) return <EmptyState title="Too long to compare here">Open the two versions and compare them in an editor.</EmptyState>
    return (
      <div className="artifact-view">
        {masked}
        <p className="faint">This version against the latest: <span className="diff-del">removed</span> lines are only here, <span className="diff-add">added</span> lines are only in the latest.</p>
        <pre className="artifact-text artifact-diff" aria-label="Changes from this version to the latest">
          {lines.map((l, i) => (
            <span key={i} className={l.op === '+' ? 'diff-add' : l.op === '-' ? 'diff-del' : undefined}>
              {l.op} {l.text}
            </span>
          ))}
        </pre>
      </div>
    )
  }
  return (
    <div className="artifact-view">
      {masked}
      {a.kind === 'markdown' && mode === 'view' ? <Markdown text={mine.text ?? ''} /> : <pre className="artifact-text">{mine.text}</pre>}
    </div>
  )
}
