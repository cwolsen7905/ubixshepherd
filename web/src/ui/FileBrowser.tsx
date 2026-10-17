import { useEffect, useState } from 'react'
import { laneFiles } from '../api/client'
import type { LaneFileNode } from '../api/types'
import { StatusGlyph } from './StatusGlyph'
import { Button } from './Button'

/**
 * A read-only file tree and viewer for a lane's worktree or any repo ref. `tree` shows only
 * the tree; `tree-viewer` adds a file reader beside it.
 */
export function FileBrowser({ laneId, gitRef, layout = 'tree-viewer' }: {
  laneId: number
  /** A branch, tag or sha; the lane's worktree when absent. */
  gitRef?: string
  layout?: 'tree' | 'tree-viewer'
}) {
  const [target, setTarget] = useState('')
  const [root, setRoot] = useState<LaneFileNode | null>(null)
  const [file, setFile] = useState<LaneFileNode | null>(null)
  const [got, setGot] = useState<{ key: string; error?: string } | null>(null)
  const key = `${laneId}|${gitRef ?? ''}|${target}`
  const loading = got?.key !== key
  const error = got?.key === key ? got.error : undefined
  const path = root?.path ?? ''

  // A new lane or ref starts again at the top.
  const scope = `${laneId}|${gitRef ?? ''}`
  const [seen, setSeen] = useState(scope)
  if (seen !== scope) {
    setSeen(scope)
    setTarget('')
    setRoot(null)
    setFile(null)
  }

  useEffect(() => {
    let current = true
    laneFiles.read(laneId, target, gitRef).then(
      (n) => {
        if (!current) return
        if (n.kind === 'dir') {
          setRoot(n)
          setFile(null)
        } else {
          setFile(n)
        }
        setGot({ key })
      },
      (e: unknown) => current && setGot({ key, error: e instanceof Error ? e.message : String(e) }),
    )
    return () => {
      current = false
    }
  }, [key, laneId, gitRef, target])

  const navigate = (child: LaneFileNode) => setTarget(child.path)

  if (error && !root) return <p className="tone-bad">Could not load the files: {error}</p>
  if (!root) return <p className="faint">Reading…</p>

  const tree = (
    <nav className="sh-file-tree" aria-label="Files">
      {path !== '' && (
        <Button variant="ghost" size="small" onClick={() => setTarget(parentPath(path))}>
          ← back
        </Button>
      )}
      <ul>
        {root.children?.map((c) => (
          <li key={c.path}>
            <button
              type="button"
              className="sh-file-tree-item"
              onClick={() => navigate(c)}
              aria-current={file?.path === c.path ? 'true' : undefined}
            >
              <StatusGlyph glyph={c.kind === 'dir' ? '▾' : '▤'} tone="idle" label={c.kind} />
              <span className="sh-file-tree-name" title={c.path}>{c.name}</span>
              {c.size !== undefined && <span className="faint">{bytes(c.size)}</span>}
            </button>
          </li>
        ))}
      </ul>
      {loading && <p className="faint">Reading…</p>}
      {error && <p className="tone-bad">Could not load the files: {error}</p>}
    </nav>
  )

  if (layout === 'tree' || file === null) {
    return (
      <div className="sh-file-browser" data-component="FileBrowser" data-layout={layout}>
        {tree}
      </div>
    )
  }

  return (
    <div className="sh-file-browser sh-file-browser--viewer" data-component="FileBrowser" data-layout={layout}>
      {tree}
      <article className="sh-file-viewer" aria-label={file.name}>
        <header className="sh-file-viewer-head">
          <b title={file.path}>{file.name}</b>
          {file.binary && <span className="sh-tag">binary</span>}
          {file.redacted ? <span className="faint">{file.redacted} masked</span> : null}
        </header>
        {file.binary ? (
          <p className="faint">This file is binary or too large to show here.</p>
        ) : (
          <pre className="sh-file-viewer-body" tabIndex={0}>{file.data ?? ''}</pre>
        )}
      </article>
    </div>
  )
}

function parentPath(p: string): string {
  const i = p.lastIndexOf('/')
  return i < 0 ? '' : p.slice(0, i)
}

function bytes(n: number): string {
  if (n < 1024) return `${n} B`
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`
  return `${(n / (1024 * 1024)).toFixed(1)} MB`
}
