import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { ApiError, config as configApi } from '../api/client'
import type { ConfigDoc, ConfigField, ConfigPreview, ConfigRefusal, ConfigRisk } from '../api/types'
import {
  deletedProjects, dotted, fieldState, listText, mapText, moveRepo, NEW_PROJECT, opsOf, ownerOf, parseList, parseMap, pickScope, PROJECT_NAME,
  problemsFor, projectRepos, removeProject, revert, scopes, setValue, show, unsetValue, type Draft, type Scope,
} from '../model/settings'
import { go, href } from '../router'
import { Button, Chip } from '../ui'
import '../styles/settings.css'

/** The word a person types to confirm a change that gives agents more power. */
export const CONFIRM_WORD = 'confirm'

const message = (e: unknown) => (e instanceof Error ? e.message : String(e))

/** A refusal's structured body, when the daemon sent one. */
function refusal(e: unknown): ConfigRefusal | null {
  if (e instanceof ApiError && e.body && typeof e.body === 'object') return e.body as ConfigRefusal
  return null
}

interface FieldProps {
  scope: Scope
  field: ConfigField
  draft: Draft
  onDraft: (d: Draft) => void
  problems: string[]
}

function FieldValue({ field, value, disabled, onChange, id }: { field: ConfigField; value: unknown; disabled: boolean; onChange: (v: unknown) => void; id: string }) {
  switch (field.kind) {
    case 'bool':
      return <input id={id} type="checkbox" checked={value !== false && value !== undefined && value !== null} disabled={disabled} onChange={(e) => onChange(e.target.checked)} />
    case 'enum':
      return (
        <select id={id} value={String(value ?? '')} disabled={disabled} onChange={(e) => onChange(e.target.value)}>
          {value === undefined || value === '' ? <option value="" /> : null}
          {(field.choices ?? []).map((c) => <option key={c} value={c}>{c}</option>)}
        </select>
      )
    case 'number':
      return (
        <input id={id} type="number" value={typeof value === 'number' ? value : String(value ?? '')} disabled={disabled}
          onChange={(e) => onChange(e.target.value === '' ? '' : Number(e.target.value))} />
      )
    case 'text':
      return <textarea id={id} rows={4} value={String(value ?? '')} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
    case 'list':
      return <textarea id={id} rows={3} value={listText(value)} disabled={disabled} placeholder="one per line" onChange={(e) => onChange(parseList(e.target.value))} />
    case 'map':
      return <textarea id={id} rows={3} value={mapText(value)} disabled={disabled} placeholder="name: value, one per line" onChange={(e) => onChange(parseMap(e.target.value))} />
    case 'json':
      return <JsonBox id={id} value={value} disabled={disabled} onChange={onChange} />
    default:
      return <input id={id} type="text" value={String(value ?? '')} disabled={disabled} onChange={(e) => onChange(e.target.value)} />
  }
}

/** A structured value as JSON text; it is sent once it parses. */
function JsonBox({ id, value, disabled, onChange }: { id: string; value: unknown; disabled: boolean; onChange: (v: unknown) => void }) {
  const [text, setText] = useState(() => JSON.stringify(value ?? null, null, 2))
  const [bad, setBad] = useState(false)
  return (
    <>
      <textarea id={id} rows={6} className="mono" value={text} disabled={disabled} aria-invalid={bad}
        onChange={(e) => {
          setText(e.target.value)
          try {
            onChange(JSON.parse(e.target.value))
            setBad(false)
          } catch {
            setBad(true)
          }
        }} />
      {bad && <span className="field-problem">Not valid JSON yet; the last valid value is kept.</span>}
    </>
  )
}

function Field({ scope, field, draft, onDraft, problems }: FieldProps) {
  const path = [...scope.root, ...field.key]
  const full = dotted(path)
  const st = fieldState(scope, field, draft)
  const id = `f-${full}`
  const inherited = scope.inherits
  const mine = problemsFor(problems, full)
  return (
    <div className={`field${st.set ? ' is-set' : ''}${st.dirty ? ' is-dirty' : ''}${field.kind === 'list' || field.kind === 'map' || field.kind === 'json' ? ' field-wide' : ''}`}>
      <div className="field-head">
        <label htmlFor={id}>{field.label}</label>
        {field.risk && <span className="tag-risk" title="Raising this gives agents more power; saving asks you to confirm"><Chip>Power</Chip></span>}
        {field.restart && <span title="Applies when the daemon next starts"><Chip>Restart</Chip></span>}
        <label className="override">
          <input type="checkbox" checked={st.set} 
            aria-label={`${inherited ? 'Override' : 'Set'} ${field.label}`}
            onChange={(e) => {
              if (!e.target.checked) {
                onDraft(unsetValue(draft, path))
                return
              }
              // Start from what is in force, so turning it on changes nothing until edited.
              const v = st.value ?? (field.kind === 'list' ? [] : field.kind === 'map' || field.kind === 'json' ? {} : field.kind === 'bool' ? false : '')
              onDraft(setValue(draft, path, v))
            }} />
          <span>{inherited ? 'Override' : 'Set'}</span>
        </label>
        {st.dirty && (
          <Button variant="ghost" size="small" onClick={() => onDraft(revert(draft, path))}>Undo</Button>
        )}
      </div>
      <div className="field-body">
        <FieldValue key={`${full}:${st.set}`} id={id} field={field} value={st.value} disabled={!st.set}
          onChange={(v) => onDraft(setValue(draft, path, v))} />
        {!st.set && (
          <span className="faint field-hint">
            {inherited ? 'inherited' : 'default'}: {show(st.value)}
          </span>
        )}
      </div>
      {field.help && <p className="field-help">{field.help}</p>}
      {mine.map((p) => <p key={p} className="field-problem" role="alert">{p}</p>)}
    </div>
  )
}

/**
 * A project's repos as checkboxes over every configured repo. A repo already in another
 * project says which; ticking it moves it, and the move is part of the same review.
 */
function RepoPicker({ project, doc, draft, added, repos, onDraft }: {
  project: string
  doc: ConfigDoc
  draft: Draft
  added: string[]
  repos: string[]
  onDraft: (d: Draft) => void
}) {
  const map = projectRepos(doc, draft, added)
  const mine = new Set(map.get(project) ?? [])
  const names = [...new Set([...repos, ...mine])].sort()
  const saved = new Set(asList(doc.projects[project]?.raw.repos))
  return (
    <fieldset className="field field-wide repo-picker">
      <legend>Repos</legend>
      {names.length === 0 && <p className="faint">No repo is configured yet. Add one first.</p>}
      <ul>
        {names.map((r) => {
          const owner = ownerOf(map, r)
          const elsewhere = owner !== undefined && owner !== project
          return (
            <li key={r}>
              <label>
                <input
                  type="checkbox"
                  checked={mine.has(r)}
                  onChange={(e) => onDraft(moveRepo(doc, draft, added, project, r, e.target.checked))}
                />{' '}
                <code>{r}</code>
              </label>{' '}
              <span className={elsewhere ? 'tone-warn' : 'faint'}>
                {mine.has(r) ? (saved.has(r) ? 'in this project' : 'added here') : elsewhere ? `in ${owner}: ticking moves it here` : 'in no project'}
              </span>
            </li>
          )
        })}
      </ul>
      <p className="field-help">A repo is in at most one project. Moving one changes both projects in the same saved change.</p>
    </fieldset>
  )
}

const asList = (v: unknown): string[] => (Array.isArray(v) ? v.map(String) : [])

/** Delete a project, after saying what happens to its repos. */
function DeleteProject({ name, repos, onDelete }: { name: string; repos: string[]; onDelete: () => void }) {
  const [asking, setAsking] = useState(false)
  if (!asking) {
    return <Button variant="danger" onClick={() => setAsking(true)}>Delete project…</Button>
  }
  return (
    <div className="banner tone-warn" role="group" aria-label={`Delete project ${name}`}>
      <p>
        Delete project <strong>{name}</strong>?{' '}
        {repos.length > 0 ? <>Its repos ({repos.join(', ')}) become Unassigned.</> : 'It has no repos.'} Its budget, caches
        and intake rules go with it, and a rule in another project that names it will stop the save until it is changed.
        Nothing is written until you review and save.
      </p>
      <Button variant="danger" onClick={onDelete}>Delete {name}</Button>{' '}
      <Button onClick={() => setAsking(false)}>Cancel</Button>
    </div>
  )
}

function Review({
  preview, risky, typed, onTyped, onSave, onClose, saving, canWrite, problems,
}: {
  preview: ConfigPreview
  risky: ConfigRisk[]
  typed: string
  onTyped: (s: string) => void
  onSave: () => void
  onClose: () => void
  saving: boolean
  canWrite: boolean
  problems: string[]
}) {
  const confirmed = risky.length === 0 || typed.trim().toLowerCase() === CONFIRM_WORD
  const lines = preview.diff.split('\n')
  return (
    <section className="review" aria-label="Review changes">
      <h2>Review changes</h2>
      {preview.diff ? (
        <pre className="diff" aria-label="Diff of config.yaml">
          {lines.map((l, i) => <span key={i} className={l.startsWith('+') ? 'diff-add' : l.startsWith('-') ? 'diff-del' : l.startsWith('@@') ? 'diff-gap' : undefined}>{l + '\n'}</span>)}
        </pre>
      ) : (
        <p className="faint">These changes leave the file as it is.</p>
      )}
      {problems.length > 0 && (
        <div className="banner tone-bad" role="alert">
          <strong>Not saved: the result is not a valid config.</strong>
          <ul>{problems.map((p) => <li key={p}>{p}</li>)}</ul>
        </div>
      )}
      {risky.length > 0 && (
        <div className="banner tone-warn" role="group" aria-label="Changes that need confirming">
          <strong>These changes give agents more power.</strong>
          <ul>
            {risky.map((r) => (
              <li key={r.path}><code>{r.path}</code>: {r.from || '(empty)'} → {r.to || '(empty)'}. {r.why}.</li>
            ))}
          </ul>
          <label>
            Type <code>{CONFIRM_WORD}</code> to allow it{' '}
            <input type="text" value={typed} onChange={(e) => onTyped(e.target.value)} autoComplete="off" />
          </label>
        </div>
      )}
      <div className="actions">
        {canWrite ? (
          <Button variant="primary" onClick={onSave} disabled={saving || !preview.diff || problems.length > 0 || !confirmed}>
            {saving ? 'Saving…' : 'Save and reload'}
          </Button>
        ) : (
          <span className="faint">This session cannot save; the diff is what a save would write.</span>
        )}
        <Button onClick={onClose}>Keep editing</Button>
      </div>
    </section>
  )
}

export function SettingsPage({ scope: scopeId }: { scope?: string }) {
  const [doc, setDoc] = useState<ConfigDoc | null>(null)
  const [loadError, setLoadError] = useState<string | null>(null)
  const [draft, setDraft] = useState<Draft>({})
  const [preview, setPreview] = useState<ConfigPreview | null>(null)
  const [problems, setProblems] = useState<string[]>([])
  const [risky, setRisky] = useState<ConfigRisk[]>([])
  const [typed, setTyped] = useState('')
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<{ tone: 'ok' | 'bad'; text: string } | null>(null)
  const [stale, setStale] = useState(false)
  const [added, setAdded] = useState<string[]>([])
  const [newRepo, setNewRepo] = useState('')
  const [addedProjects, setAddedProjects] = useState<string[]>([])
  const [newProject, setNewProject] = useState('')
  const [projectProblem, setProjectProblem] = useState<string | null>(null)
  const projectBox = useRef<HTMLInputElement>(null)

  const load = useCallback(() => {
    configApi.get().then(
      (d) => {
        setDoc(d)
        setLoadError(null)
        setStale(false)
      },
      (e: unknown) => setLoadError(message(e)),
    )
  }, [])
  useEffect(() => load(), [load])
  // "New project" on the Projects page lands here with the add box ready.
  const loaded = doc !== null
  useEffect(() => {
    if (scopeId === NEW_PROJECT && loaded) projectBox.current?.focus()
  }, [scopeId, loaded])

  const gone = useMemo(() => deletedProjects(draft), [draft])
  const all = useMemo(
    () => (doc ? scopes(doc, added, addedProjects).filter((s) => !(s.id.startsWith('project:') && gone.has(s.id.slice('project:'.length)))) : []),
    [doc, added, addedProjects, gone],
  )
  const scope = pickScope(all, scopeId)

  if (loadError && !doc) return <div className="page settings-page"><p className="banner tone-bad" role="alert">Settings did not load: {loadError}</p></div>
  if (!doc || !scope) return <div className="page settings-page"><p className="faint">Loading settings…</p></div>

  const readOnly = !doc.can_write
  const ops = opsOf(draft)
  const change = () => ({ etag: doc.etag, ops })

  const onDraft = (d: Draft) => {
    setDraft(d)
    setPreview(null)
    setProblems([])
    setRisky([])
    setTyped('')
    setNotice(null)
  }

  const onRefused = (e: unknown, previewing: boolean) => {
    const r = refusal(e)
    if (e instanceof ApiError && e.status === 409) {
      setStale(true)
      setPreview(null)
      return
    }
    if (e instanceof ApiError && e.status === 422 && r?.problems) {
      setProblems(r.problems)
      // The diff is of an invalid file: show what was tried as errors only.
      setPreview({ etag: doc.etag, diff: previewing ? '' : (preview?.diff ?? '') })
      return
    }
    if (e instanceof ApiError && e.status === 428 && r?.risky) {
      setRisky(r.risky)
      return
    }
    setNotice({ tone: 'bad', text: message(e) })
  }

  const review = async () => {
    setBusy(true)
    setProblems([])
    setNotice(null)
    try {
      const p = await configApi.preview(change())
      setPreview(p)
      setRisky(p.risky ?? [])
    } catch (e) {
      onRefused(e, true)
      if (!(e instanceof ApiError && e.status === 422)) setPreview(null)
      else setPreview({ etag: doc.etag, diff: '' })
    } finally {
      setBusy(false)
    }
  }

  const save = async () => {
    setBusy(true)
    try {
      const out = await configApi.save({ ...change(), confirm: risky.map((r) => r.path) })
      setDraft({})
      setPreview(null)
      setRisky([])
      setTyped('')
      setAdded([])
      setAddedProjects([])
      setNotice(out.reloaded ? { tone: 'ok', text: 'Saved and reloaded. Running agents keep what they started with.' } : { tone: 'bad', text: `Saved, but the daemon did not reload it: ${out.reload_error ?? 'unknown error'}` })
      load()
    } catch (e) {
      onRefused(e, false)
    } finally {
      setBusy(false)
    }
  }

  const isProject = scope.id.startsWith('project:')
  const projectName = isProject ? scope.id.slice('project:'.length) : ''
  const groups = new Map<string, ConfigField[]>()
  for (const f of scope.section.fields) {
    const g = f.group ?? ''
    groups.set(g, [...(groups.get(g) ?? []), f])
  }
  const nav = (heading: string, prefix: string) => {
    const items = all.filter((s) => (prefix ? s.id.startsWith(prefix) : !s.id.includes(':')))
    return items.length === 0 ? null : (
      <>
        {heading && <h2>{heading}</h2>}
        <ul>
          {items.map((s) => (
            <li key={s.id}>
              <a href={href.settings(s.id)} aria-current={s.id === scope.id ? 'page' : undefined}>{s.label}</a>
            </li>
          ))}
        </ul>
      </>
    )
  }

  return (
    <div className="page page-wide settings-page">
      <div className="settings-layout">
        <nav className="settings-nav" aria-label="Settings">
          {nav('', '')}
          {nav('Repos', 'repo:')}
          {!readOnly && (
            <form className="add-repo" onSubmit={(e) => {
              e.preventDefault()
              const n = newRepo.trim()
              if (!n) return
              setAdded((a) => (a.includes(n) ? a : [...a, n]))
              setNewRepo('')
              go(href.settings(`repo:${n}`))
            }}>
              <label className="visually-hidden" htmlFor="new-repo">Repo name</label>
              <input id="new-repo" value={newRepo} placeholder="add a repo" onChange={(e) => setNewRepo(e.target.value)} />
              <Button type="submit" size="small">Add</Button>
            </form>
          )}
          {nav('Projects', 'project:')}
          {!readOnly && (
            <form className="add-repo" onSubmit={(e) => {
              e.preventDefault()
              const n = newProject.trim()
              if (!n) return
              if (!PROJECT_NAME.test(n)) {
                setProjectProblem('A project name uses letters, digits, dots, dashes and underscores.')
                return
              }
              setProjectProblem(null)
              setNewProject('')
              if (!(n in doc.projects) && !addedProjects.includes(n)) {
                setAddedProjects((a) => [...a, n])
                // An entry with its list of repos, so the project is written even if empty.
                onDraft(setValue(draft, ['projects', n, 'repos'], []))
              }
              go(href.settings(`project:${n}`))
            }}>
              <label className="visually-hidden" htmlFor="new-project">Project name</label>
              <input id="new-project" ref={projectBox} value={newProject} placeholder="add a project" aria-invalid={projectProblem !== null}
                onChange={(e) => setNewProject(e.target.value)} />
              <Button type="submit" size="small">Add</Button>
            </form>
          )}
          {projectProblem && <p className="field-problem" role="alert">{projectProblem}</p>}
        </nav>
        <div className="settings-main">
          <h1>{scope.section.label}{scope.id.includes(':') ? `: ${scope.label}` : ''}</h1>
          <p className="faint">
            Editing <code>{doc.path}</code>{doc.missing ? ' (not written yet)' : ''}. Comments and the order of the file are kept; a backup of the previous file is kept beside it.
          </p>
          {readOnly && <p className="banner tone-warn" role="status">{doc.why ?? 'This session cannot save settings.'} You can still change fields and review the diff.</p>}
          {doc.invalid && doc.invalid.length > 0 && (
            <div className="banner tone-bad" role="alert">
              The file on disk is not a valid config, so what is shown is what the daemon is running on:
              <ul>{doc.invalid.map((p) => <li key={p}>{p}</li>)}</ul>
            </div>
          )}
          {stale && (
            <p className="banner tone-bad" role="alert">
              config.yaml changed on disk since it was read. Nothing was saved.{' '}
              <Button size="small" onClick={load}>Reload it</Button>, then review your changes again.
            </p>
          )}
          {notice && <p className={`banner tone-${notice.tone === 'ok' ? 'ok' : 'bad'}`} role="status">{notice.text}</p>}
          {scope.section.fields.length === 0 && <p className="faint">Nothing to set here.</p>}
          {[...groups].map(([g, fields]) => (
            <fieldset key={g} className="group">
              {g && <legend>{g}</legend>}
              <div className="field-grid">
              {fields.map((f) =>
                isProject && f.key.join('.') === 'repos' ? (
                  <RepoPicker key="repos" project={projectName} doc={doc} draft={draft} added={addedProjects} repos={Object.keys(doc.repos).concat(added)} onDraft={onDraft} />
                ) : (
                  <Field key={f.key.join('.')} scope={scope} field={f} draft={draft} onDraft={onDraft} problems={problems} />
                ),
              )}
              </div>
            </fieldset>
          ))}
          {isProject && !readOnly && (
            <DeleteProject
              key={projectName}
              name={projectName}
              repos={projectRepos(doc, draft, addedProjects).get(projectName) ?? []}
              onDelete={() => {
                onDraft(removeProject(doc, draft, projectName))
                setAddedProjects((a) => a.filter((n) => n !== projectName))
                go(href.settings())
              }}
            />
          )}
          <div className="actions sticky">
            <span className="faint">{ops.length === 0 ? 'No changes' : `${ops.length} change${ops.length === 1 ? '' : 's'}`}</span>
            {/* One primary per view: Save and reload takes it once the review is open. */}
            <Button variant={preview ? 'secondary' : 'primary'} disabled={ops.length === 0 || busy} onClick={review}>Review changes</Button>
            <Button variant="ghost" disabled={ops.length === 0} onClick={() => onDraft({})}>Discard</Button>
          </div>
          {preview && (
            <Review preview={preview} risky={risky} typed={typed} onTyped={setTyped} onSave={save} onClose={() => setPreview(null)}
              saving={busy} canWrite={doc.can_write} problems={problems} />
          )}
        </div>
      </div>
    </div>
  )
}
