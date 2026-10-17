import { useCallback, useEffect, useState } from 'react'
import type { DiffFile, LaneDiff } from '../api/types'
import { laneChanges } from '../api/client'
import { shortSha } from '../format'
import { editorHref, joinPath } from '../editorLink'
import { StatusGlyph } from './StatusGlyph'

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

export interface RunRange {
  id: number
  latest: boolean
  outside?: string[]
}

type Variant = 'list' | 'diff'

/**
 * A lane's changes: commits, and committed / uncommitted / untracked file sections. The `diff`
 * variant lets each file expand to its patch; the `list` variant keeps rows collapsed.
 */
export function ChangesList({ read, worktree, editorUrl, run, variant = 'list' }: {
  read: LaneDiff
  worktree: string
  editorUrl: string | null
  run?: RunRange
  variant?: Variant
}) {
  const outside = new Set(run?.outside ?? [])
  const marked = (f: DiffFile): DiffFile => (outside.has(f.path) && f.in_scope ? { ...f, in_scope: false } : f)
  const groups = GROUPS.filter((g) => !run || run.latest || g.key === 'committed')
  const lists = {
    committed: read.committed.map(marked),
    uncommitted: run && !run.latest ? [] : read.uncommitted.map(marked),
    untracked: run && !run.latest ? [] : read.untracked.map(marked),
  }
  const files = lists.committed.length + lists.uncommitted.length + lists.untracked.length
  const outsideCount = Object.values(lists).flat().filter((f) => !f.in_scope).length
  return (
    <div className="changes" data-component="ChangesList" data-variant={variant}>
      <p className="changes-summary">
        {run && <>Run {run.id}: </>}
        <code>{read.branch}</code> at <code>{shortSha(read.head)}</code> against <code>{read.base_ref || read.base}</code>
        {' · '}{read.commits_total} {read.commits_total === 1 ? 'commit' : 'commits'}, {files}{read.files_more ? `+${read.files_more}` : ''} {files === 1 ? 'file' : 'files'}
        {outsideCount > 0 && <> · <span className="tone-warn">{outsideCount} outside scope</span></>}
        {read.gate && (
          <>
            {' · '}
            <StatusGlyph glyph="✓" tone={read.gate.state === 'passed' ? 'ok' : read.gate.state === 'failed' ? 'bad' : 'warn'} label={`gate ${read.gate.state}`} />
          </>
        )}
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

      {groups.map((g) => {
        const list = lists[g.key]
        const laneNote = run && g.key !== 'committed'
        return (
          <section key={g.key} aria-labelledby={`changes-${g.key}`}>
            <h2 id={`changes-${g.key}`} className="sh-changes-h">
              {g.title} <span className="sh-group-h-n">{list.length}</span>
              {laneNote && <span className="faint"> in the lane's worktree now</span>}
            </h2>
            {list.length === 0 ? (
              <p className="faint">{g.empty}</p>
            ) : (
              <ul className="sh-changes">
                {list.map((f) => (
                  <FileRow key={`${g.key}:${f.path}`} file={f} group={g.key} read={read} worktree={worktree} editorUrl={editorUrl} runId={run?.id} variant={variant} />
                ))}
              </ul>
            )}
          </section>
        )
      })}
    </div>
  )
}

function FileRow({ file, group, read, worktree, editorUrl, runId, variant }: {
  file: DiffFile
  group: (typeof GROUPS)[number]['key']
  read: LaneDiff
  worktree: string
  editorUrl: string | null
  runId?: number
  variant: Variant
}) {
  const [open, setOpen] = useState(false)
  const [patch, setPatch] = useState<{ at: string; read: LaneDiff } | null>(null)
  const [error, setError] = useState<string | null>(null)
  const readAt = read.read_at
  const body = !file.binary && variant === 'diff'
  const load = useCallback(() => {
    let current = true
    laneChanges.diff(read.lane_id, file.path, runId).then(
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
  }, [read.lane_id, file.path, readAt, runId])
  useEffect(() => (open && body ? load() : undefined), [open, body, load])

  const link = file.status === 'deleted' ? null : editorHref(editorUrl, joinPath(worktree, file.path))
  const id = `patch-${group}-${file.path}`
  const mark = STATUS_MARK[file.status] ?? '·'
  const scope: 'A' | 'M' | 'D' | 'U' = file.status === 'added' || file.status === 'untracked' ? 'A'
    : file.status === 'deleted' ? 'D'
    : file.status === 'modified' || file.status === 'renamed' || file.status === 'copied' || file.status === 'typechange' ? 'M' : 'U'
  const counts = file.binary ? `binary, ${file.size ?? 0} bytes`
    : file.status === 'untracked' ? `${file.size ?? 0} bytes`
    : `+${file.added}/-${file.removed}`
  return (
    <li className={`sh-change${file.in_scope ? '' : ' is-outside'}`} data-component="ChangeRow" title={file.in_scope ? undefined : `${file.path}: outside scope`}>
      <span className={`sh-change-s--${scope}`} aria-hidden="true">{mark}</span>
      <span className="sh-change-path" title={file.old_path ? `${file.old_path} → ${file.path}` : file.path}>
        {variant === 'diff' ? (
          <>
            <button
              type="button"
              className="sh-change-open"
              aria-expanded={open}
              aria-controls={id}
              onClick={() => setOpen((o) => !o)}
            >
              {file.old_path && <span className="faint">{file.old_path} → </span>}
              {file.path}
            </button>
            {!file.in_scope && <span className="sh-change-scope">outside scope</span>}
          </>
        ) : (
          <>
            {file.old_path && <span className="faint">{file.old_path} → </span>}
            {file.path}
            {!file.in_scope && <span className="sh-change-scope">outside scope</span>}
          </>
        )}
      </span>
      <span className="sh-change-n num">
        {file.binary || file.status === 'untracked' ? (
          <span className="faint">{counts}</span>
        ) : (
          <>
            <span className="sh-add">+{file.added}</span>/<span className="sh-del">-{file.removed}</span>
          </>
        )}
      </span>
      <span className="sh-change-open-col">
        {link && (
          <a className="sh-change-open-link" href={link} title={`Open in editor. ${DAEMON_PATH_NOTE}`} onClick={(e) => e.stopPropagation()}>
            open<span className="visually-hidden"> {file.path} in editor</span>
          </a>
        )}
      </span>
      {open && variant === 'diff' && (
        <div id={id} className="sh-change-patch">
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

export function lineClass(line: string): string {
  if (line.startsWith('@@')) return 'patch-hunk'
  if (/^(diff --git|index |--- |\+\+\+ |new file|deleted file|similarity|rename |old mode|new mode|Binary files)/.test(line)) return 'patch-meta'
  if (line.startsWith('+')) return 'patch-add'
  if (line.startsWith('-')) return 'patch-del'
  return 'patch-ctx'
}

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
