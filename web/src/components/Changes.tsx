import { useCallback, useEffect, useState } from 'react'
import { laneChanges } from '../api/client'
import type { DiffFile, LaneDiff } from '../api/types'
import { clockTime, shortSha } from '../format'
import { editorHref, joinPath } from '../editorLink'
import '../styles/changes.css'

const GROUPS = [
  { key: 'committed', title: 'Committed', empty: 'Nothing committed against the base.' },
  { key: 'uncommitted', title: 'Uncommitted', empty: 'No uncommitted changes to tracked files.' },
  { key: 'untracked', title: 'Untracked', empty: 'No untracked files.' },
] as const

const STATUS_MARK: Record<string, string> = {
  added: 'A', modified: 'M', deleted: 'D', renamed: 'R', copied: 'C', typechange: 'T', untracked: '?',
}

const DAEMON_PATH_NOTE = 'The path is on the machine the daemon runs on; the editor opens it on this one.'

function message(e: unknown): string {
  return e instanceof Error ? e.message : String(e)
}

/**
 * The lane's Changes tab: what the lane holds against its base. The read is taken when the
 * tab opens, when `refreshKey` moves (the feed advanced) and on Refresh; it is not polled,
 * since each read runs git. A file's patch is read only when it is opened.
 */
export function Changes({ laneId, worktree, editorUrl, refreshKey, now }: {
  laneId: number
  worktree: string
  /** The editor link template; "" or null hides the links. */
  editorUrl: string | null
  refreshKey: number
  now: number
}) {
  const [read, setRead] = useState<LaneDiff | null>(null)
  const [error, setError] = useState<string | null>(null)
  // The request being waited on is the one named by the lane, the feed and Refresh.
  const [manual, setManual] = useState(0)
  const request = `${laneId}:${refreshKey}:${manual}`
  const [answered, setAnswered] = useState('')
  const busy = answered !== request

  useEffect(() => {
    let current = true
    laneChanges.diff(laneId).then(
      (d) => {
        if (!current) return
        setRead(d)
        setError(null)
      },
      (e: unknown) => current && setError(message(e)),
    ).finally(() => current && setAnswered(request))
    return () => {
      current = false
    }
  }, [laneId, request])

  return (
    <div id="lane-panel" role="tabpanel" aria-labelledby="lane-tab-changes" className="changes">
      <div className="changes-bar">
        <span className="faint">
          {read ? <>Read <time dateTime={read.read_at} title={read.read_at}>{clockTime(read.read_at, now)}</time></> : busy ? 'Reading…' : ''}
        </span>
        <button type="button" className="link-button" disabled={busy} onClick={() => setManual((n) => n + 1)}>
          {busy ? 'Reading…' : 'Refresh'}
        </button>
      </div>
      {error && <p className="tone-bad" role="alert">Could not read the lane's changes: {error}</p>}
      {read && <ChangesView read={read} worktree={worktree} editorUrl={editorUrl} />}
    </div>
  )
}

/** A read, drawn. It holds no fetching of its own but each file's patch, so two reads can sit side by side later. */
export function ChangesView({ read, worktree, editorUrl }: { read: LaneDiff; worktree: string; editorUrl: string | null }) {
  const files = read.committed.length + read.uncommitted.length + read.untracked.length
  return (
    <>
      <p className="changes-summary">
        <code>{read.branch}</code> at <code>{shortSha(read.head)}</code> against <code>{read.base_ref || read.base}</code>
        {' · '}{read.commits_total} {read.commits_total === 1 ? 'commit' : 'commits'}, {files}{read.files_more ? `+${read.files_more}` : ''} {files === 1 ? 'file' : 'files'}
        {read.out_of_scope > 0 && <> · <span className="tone-warn">{read.out_of_scope} outside scope</span></>}
      </p>
      {(read.truncated || (read.notes?.length ?? 0) > 0) && (
        <ul className="changes-notes faint">
          {read.notes?.map((n) => <li key={n}>{n}</li>)}
        </ul>
      )}
      {read.redacted ? <p className="faint">{read.redacted} {read.redacted === 1 ? 'span' : 'spans'} masked: this view is not the raw bytes.</p> : null}

      <h2 className="section-h">Commits</h2>
      {read.commits.length === 0 ? (
        <p className="faint">No commits ahead of the base.</p>
      ) : (
        <>
          {read.commits_total > read.commits.length && (
            <p className="faint">The newest {read.commits.length} of {read.commits_total}.</p>
          )}
          <ol className="change-commits">
            {read.commits.map((c) => (
              <li key={c.sha}>
                <code title={c.sha}>{shortSha(c.sha)}</code>
                <span className="change-subject">{c.subject}</span>
                <span className="faint">{c.files} {c.files === 1 ? 'file' : 'files'}</span>
              </li>
            ))}
          </ol>
        </>
      )}

      {GROUPS.map((g) => {
        const list = read[g.key]
        return (
          <section key={g.key} aria-labelledby={`changes-${g.key}`}>
            <h2 id={`changes-${g.key}`} className="section-h">
              {g.title} <span className="group-count">{list.length}</span>
            </h2>
            {list.length === 0 ? (
              <p className="faint">{g.empty}</p>
            ) : (
              <ul className="change-files">
                {list.map((f) => (
                  <FileRow key={`${g.key}:${f.path}`} file={f} group={g.key} read={read} worktree={worktree} editorUrl={editorUrl} />
                ))}
              </ul>
            )}
          </section>
        )
      })}
    </>
  )
}

function FileRow({ file, group, read, worktree, editorUrl }: {
  file: DiffFile
  group: (typeof GROUPS)[number]['key']
  read: LaneDiff
  worktree: string
  editorUrl: string | null
}) {
  const [open, setOpen] = useState(false)
  const [patch, setPatch] = useState<{ at: string; read: LaneDiff } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const readAt = read.read_at
  const body = !file.binary
  const load = useCallback(() => {
    let current = true
    laneChanges.diff(read.lane_id, file.path).then(
      (d) => {
        if (!current) return
        setPatch({ at: readAt, read: d })
        setError(null)
      },
      (e: unknown) => current && setError(message(e)),
    )
    return () => {
      current = false
    }
  }, [read.lane_id, file.path, readAt])
  // Open files are read again with each new read of the list, so they never show a stale patch.
  useEffect(() => (open && body ? load() : undefined), [open, body, load])

  const link = file.status === 'deleted' ? null : editorHref(editorUrl, joinPath(worktree, file.path))
  const id = `patch-${group}-${file.path}`
  return (
    <li className={`change-file${file.in_scope ? '' : ' is-outside'}`}>
      <div className="change-row">
        <button
          type="button"
          className="change-toggle"
          aria-expanded={open}
          aria-controls={id}
          onClick={() => setOpen((o) => !o)}
        >
          <abbr className={`change-status status-${file.status}`} title={file.status}>{STATUS_MARK[file.status] ?? '·'}</abbr>
          <span className="change-path">
            {file.old_path && <span className="faint">{file.old_path} → </span>}
            {file.path}
          </span>
        </button>
        {!file.in_scope && <span className="tone-warn change-scope">outside scope</span>}
        <span className="change-counts num">
          {file.binary ? (
            <span className="faint">binary, {file.size ?? 0} bytes</span>
          ) : file.status === 'untracked' ? (
            <span className="faint">{file.size ?? 0} bytes</span>
          ) : (
            <>
              <span className="count-add">+{file.added}</span> <span className="count-del">-{file.removed}</span>
            </>
          )}
        </span>
        {link && (
          <a className="change-edit" href={link} title={`Open in editor. ${DAEMON_PATH_NOTE}`}>
            open<span className="visually-hidden"> {file.path} in editor</span>
          </a>
        )}
      </div>
      {open && (
        <div id={id} className="change-patch">
          {file.binary ? (
            <p className="faint">Binary file, {file.size ?? 0} bytes: no content shown.</p>
          ) : error ? (
            <p className="tone-bad" role="alert">Could not read the patch: {error}</p>
          ) : !patch ? (
            <p className="faint">Reading…</p>
          ) : (
            <Patch read={patch.read} plain={group === 'untracked'} />
          )}
        </div>
      )}
    </li>
  )
}

/** The class of one patch line. */
export function lineClass(line: string): string {
  if (line.startsWith('@@')) return 'patch-hunk'
  if (/^(diff --git|index |--- |\+\+\+ |new file|deleted file|similarity|rename |old mode|new mode|Binary files)/.test(line)) return 'patch-meta'
  if (line.startsWith('+')) return 'patch-add'
  if (line.startsWith('-')) return 'patch-del'
  return 'patch-ctx'
}

/** One file's patch as a unified diff; an untracked file's start is shown as it is. */
export function Patch({ read, plain }: { read: LaneDiff; plain?: boolean }) {
  const text = read.patch ?? ''
  const lines = text.replace(/\n$/, '').split('\n')
  return (
    <>
      {text === '' ? (
        <p className="faint">No textual change to show.</p>
      ) : (
        <pre className="patch" tabIndex={0} aria-label={`Patch of ${read.file ?? 'file'}`}>
          {lines.map((l, i) => (
            <span key={i} className={plain ? 'patch-ctx' : lineClass(l)}>{l === '' ? ' ' : l}{'\n'}</span>
          ))}
        </pre>
      )}
      {read.notes?.map((n) => <p key={n} className="faint">{n}</p>)}
    </>
  )
}
