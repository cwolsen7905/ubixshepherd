import { useCallback, useEffect, useMemo, useState } from 'react'
import { ApiError, config as configApi } from '../api/client'
import type { ConfigDoc, ConfigField, ConfigPreview, ConfigRefusal, ConfigRisk } from '../api/types'
import {
  dotted, fieldState, listText, mapText, opsOf, parseList, parseMap, pickScope, problemsFor, revert, scopes, setValue,
  show, unsetValue, type Draft, type Scope,
} from '../model/settings'
import { go, href } from '../router'
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
    <div className={`field${st.set ? ' is-set' : ''}${st.dirty ? ' is-dirty' : ''}`}>
      <div className="field-head">
        <label htmlFor={id}>{field.label}</label>
        {field.risk && <span className="tag tag-risk" title="Raising this gives agents more power; saving asks you to confirm">power</span>}
        {field.restart && <span className="tag" title="Applies when the daemon next starts">restart</span>}
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
          <button type="button" className="link" onClick={() => onDraft(revert(draft, path))}>Undo</button>
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
          <button type="button" className="primary" onClick={onSave} disabled={saving || !preview.diff || problems.length > 0 || !confirmed}>
            {saving ? 'Saving…' : 'Save and reload'}
          </button>
        ) : (
          <span className="faint">This session cannot save; the diff is what a save would write.</span>
        )}
        <button type="button" onClick={onClose}>Keep editing</button>
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

  const all = useMemo(() => (doc ? scopes(doc, added) : []), [doc, added])
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
      setNotice(out.reloaded ? { tone: 'ok', text: 'Saved and reloaded. Running agents keep what they started with.' } : { tone: 'bad', text: `Saved, but the daemon did not reload it: ${out.reload_error ?? 'unknown error'}` })
      load()
    } catch (e) {
      onRefused(e, false)
    } finally {
      setBusy(false)
    }
  }

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
              <button type="submit">Add</button>
            </form>
          )}
          {nav('Projects', 'project:')}
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
              <button type="button" onClick={load}>Reload it</button>, then review your changes again.
            </p>
          )}
          {notice && <p className={`banner tone-${notice.tone === 'ok' ? 'ok' : 'bad'}`} role="status">{notice.text}</p>}
          {scope.section.fields.length === 0 && <p className="faint">Nothing to set here.</p>}
          {[...groups].map(([g, fields]) => (
            <fieldset key={g} className="group">
              {g && <legend>{g}</legend>}
              {fields.map((f) => (
                <Field key={f.key.join('.')} scope={scope} field={f} draft={draft} onDraft={onDraft} problems={problems} />
              ))}
            </fieldset>
          ))}
          <div className="actions sticky">
            <span className="faint">{ops.length === 0 ? 'No changes' : `${ops.length} change${ops.length === 1 ? '' : 's'}`}</span>
            <button type="button" className="primary" disabled={ops.length === 0 || busy} onClick={review}>Review changes</button>
            <button type="button" disabled={ops.length === 0} onClick={() => onDraft({})}>Discard</button>
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
