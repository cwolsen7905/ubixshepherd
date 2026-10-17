import { useMemo, useState } from 'react'
import { go, href } from '../router'
import { useSnapshot } from '../state/context'
import { Button, EmptyState } from '../ui'
import { FileBrowser } from '../ui/FileBrowser'
import './Files.css'

/** Read-only files of any managed repo: a lane's worktree, or a branch, tag or sha read through one of the repo's lanes. */
export function FilesPage({ repo = 0, lane = 0, gitRef = '' }: { repo?: number; lane?: number; gitRef?: string }) {
  const snap = useSnapshot()
  const repos = useMemo(() => {
    const m = new Map<number, string>()
    for (const l of snap.lanes) m.set(l.repo_id, l.repo)
    return [...m].map(([id, name]) => ({ id, name })).sort((a, b) => a.name.localeCompare(b.name))
  }, [snap.lanes])
  const chosen = snap.lanes.find((l) => l.id === lane)
  const repoId = repo || chosen?.repo_id || 0
  const lanes = snap.lanes.filter((l) => l.repo_id === repoId)
  const target = chosen && chosen.repo_id === repoId ? chosen : (lanes[0] ?? null)
  const [draft, setDraft] = useState(gitRef)
  // The address is the truth: a repo or lane change that clears the ref clears the field.
  const [seen, setSeen] = useState(gitRef)
  if (seen !== gitRef) {
    setSeen(gitRef)
    setDraft(gitRef)
  }

  const pick = (o: { repo?: number; lane?: number; ref?: string }) => go(href.files(o))

  return (
    <div className="page page-wide files-page" data-component="Files">
      <header className="page-head">
        <h1>Files</h1>
        <p className="page-sub faint">Read-only. Secrets in a file are masked.</p>
      </header>
      {repos.length === 0 ? (
        <EmptyState title={snap.ready ? 'No repo to browse.' : 'Loading…'}>
          {snap.ready && 'A repo is browsable once it has a lane. Open one with shepherd lane open.'}
        </EmptyState>
      ) : (
        <>
          <form
            className="files-pick"
            onSubmit={(e) => {
              e.preventDefault()
              pick({ repo: repoId, lane: target?.id, ref: draft.trim() })
            }}
          >
            <label>
              <span className="faint">Repo</span>
              <select value={repoId} onChange={(e) => pick({ repo: Number(e.target.value) })}>
                <option value={0}>Choose a repo</option>
                {repos.map((r) => (
                  <option key={r.id} value={r.id}>{r.name}</option>
                ))}
              </select>
            </label>
            <label>
              <span className="faint">Lane</span>
              <select value={target?.id ?? 0} disabled={lanes.length === 0} onChange={(e) => pick({ repo: repoId, lane: Number(e.target.value), ref: gitRef })}>
                {lanes.map((l) => (
                  <option key={l.id} value={l.id}>{l.name}{l.state === 'open' ? '' : ' (closed)'}</option>
                ))}
              </select>
            </label>
            <label>
              <span className="faint">Ref</span>
              <input value={draft} placeholder="worktree, or a branch, tag or sha" onChange={(e) => setDraft(e.target.value)} />
            </label>
            <Button type="submit" variant="secondary" size="small">Show</Button>
          </form>
          {!target ? (
            <EmptyState title="Choose a repo.">Pick one above to read its files.</EmptyState>
          ) : (
            <FileBrowser
              laneId={target.id}
              gitRef={gitRef || (target.state === 'open' ? undefined : target.branch)}
              layout="tree-viewer"
            />
          )}
        </>
      )}
    </div>
  )
}
