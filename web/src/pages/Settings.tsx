import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react'
import { ApiError, config as configApi, daemonInfo, history } from '../api/client'
import type { RunRecord, ConfigDoc, ConfigField, ConfigPreview, ConfigRefusal, ConfigRisk, StatusDetail } from '../api/types'
import { clockTime, since, usd } from '../format'
import { mergeHistories, rangeStart } from '../model/history'
import { useNow } from '../hooks'
import {
  deletedProjects, dotted, fieldState, getAt, listText, mapText, moveRepo, NEW_PROJECT, opsOf, ownerOf, parseList, parseMap, pickScope, PROJECT_NAME,
  problemsFor, projectRepos, removeProject, revert, scopes, setValue, show, unsetValue, type Draft, type Scope,
} from '../model/settings'
import { go, href } from '../router'
import { Button, Chip } from '../ui'
import { Confirm } from '../ui/Confirm'
import { Kbd } from '../ui/Kbd'
import { StatusGlyph } from '../ui/StatusGlyph'
import './Settings.css'

const RISK_HINT = 'Raising this gives agents more power; saving asks you to confirm.'
const RESTART_HINT = 'Applies when the daemon next starts.'

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
        {field.risk && (
          <span className="tag-risk" title={RISK_HINT}>
            <Chip>Power</Chip><span className="visually-hidden"> {RISK_HINT}</span>
          </span>
        )}
        {field.restart && (
          <span title={RESTART_HINT}>
            <Chip>Restart</Chip><span className="visually-hidden"> {RESTART_HINT}</span>
          </span>
        )}
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

// ---- The view: sections of staged rows, then the diff bar. ----

export type Theme = 'system' | 'light' | 'dark'
export const THEME_KEY = 'sh-theme'
const THEMES: [Theme, string][] = [['system', 'System'], ['light', 'Light'], ['dark', 'Dark']]

export function readTheme(): Theme {
  try {
    const v = window.localStorage.getItem(THEME_KEY)
    return v === 'light' || v === 'dark' ? v : 'system'
  } catch {
    return 'system'
  }
}

/** System leaves the choice to the OS (no attribute); light and dark force it. This browser only. */
export function applyTheme(t: Theme) {
  if (t === 'system') document.documentElement.removeAttribute('data-theme')
  else document.documentElement.setAttribute('data-theme', t)
}
// The page that sets a theme is not the page that first shows one: apply the remembered choice as this module loads.
if (typeof document !== 'undefined') applyTheme(readTheme())

const AGENTS = ['claude', 'copilot', 'cursor', 'opencode']

const KEYS: [string, string[]][] = [
  ['Search or act', ['⌘', 'K']], ['Next waiting item', [']']], ['Move down', ['j']], ['Move up', ['k']],
  ['Open', ['Enter']], ['Pick option', ['1–9']], ['Send, then next', ['⌘', 'Enter']], ['Close', ['Esc']], ['Keys', ['?']],
]

/** The model as the sub-line shows it: no provider prefix. */
const bareModel = (m: string) => m.slice(m.indexOf('/') + 1)

/** An agent's sub-line: today's lanes and spend, or when its quota hold lifts. */
function AgentLine({ agent, model, today, hold, now }: { agent: string; model: string; today?: AgentToday; hold?: { until: string }; now: number }) {
  if (hold) return <div className="sh-setting-desc" data-agent={agent}><StatusGlyph glyph="‖" tone="idle" label="Held" /> Out of quota until {clockTime(hold.until, now)}</div>
  if (!today && !model) return <div className="sh-setting-desc" data-agent={agent}>Uses its CLI’s own default model</div>
  const { lanes, usd: spent, more } = today ?? { lanes: 0, usd: 0, more: false }
  return <div className="sh-setting-desc" data-agent={agent}>{lanes}{more ? '+' : ''} {lanes === 1 && !more ? 'lane' : 'lanes'} · {usd(spent) || '$0.00'} today</div>
}

function SettingRow({ label, desc, was, children, name }: { label: ReactNode; desc?: ReactNode; was?: string; children: ReactNode; name?: string }) {
  return (
    <div className={`sh-setting${was !== undefined ? ' is-edited' : ''}`} data-component="SettingRow" data-setting={name}>
      <div>
        <div className="sh-setting-label">{label}</div>
        {desc && <div className="sh-setting-desc">{desc}</div>}
        {was !== undefined && <div className="sh-setting-was">Not saved · was {was}</div>}
      </div>
      {children}
    </div>
  )
}

function Stepper({ label, value, step, min, format, onChange }: { label: string; value: number; step: number; min: number; format: (n: number) => string; onChange: (n: number) => void }) {
  const round = (n: number) => Math.round(n * 100) / 100
  return (
    <span className="sh-stepper" data-component="Stepper">
      <button type="button" aria-label={`Less ${label}`} disabled={value <= min} onClick={() => onChange(Math.max(min, round(value - step)))}>−</button>
      <span className="sh-stepper-value">{format(value)}</span>
      <button type="button" aria-label={`More ${label}`} onClick={() => onChange(round(value + step))}>+</button>
    </span>
  )
}

function Segmented<T extends string>({ label, value, options, onPick }: { label: string; value: T; options: [T, string][]; onPick: (v: T) => void }) {
  return (
    <span className="sh-segmented" role="group" aria-label={label} data-component="Segmented">
      {options.map(([v, text]) => (
        <button key={v} type="button" aria-pressed={v === value} onClick={() => onPick(v)}>{text}</button>
      ))}
    </span>
  )
}

/** A dropdown button: the current choice with a caret, and the others in a menu under it. */
function Menu<T extends string>({ label, value, options, onPick }: { label: string; value: T; options: [T, string][]; onPick: (v: T) => void }) {
  const [open, setOpen] = useState(false)
  const root = useRef<HTMLSpanElement>(null)
  const trigger = useRef<HTMLButtonElement>(null)
  useEffect(() => {
    if (!open) return
    root.current?.querySelector<HTMLButtonElement>('[aria-checked="true"]')?.focus()
    const away = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false)
    }
    const key = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false)
        trigger.current?.focus()
        return
      }
      const items = [...(root.current?.querySelectorAll<HTMLButtonElement>('[role="menuitemradio"]') ?? [])]
      const at = items.indexOf(document.activeElement as HTMLButtonElement)
      const to = e.key === 'ArrowDown' ? (at + 1) % items.length : e.key === 'ArrowUp' ? (at - 1 + items.length) % items.length : e.key === 'Home' ? 0 : e.key === 'End' ? items.length - 1 : -1
      if (to < 0 || items.length === 0) return
      e.preventDefault()
      items[to]?.focus()
    }
    document.addEventListener('mousedown', away)
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('mousedown', away)
      document.removeEventListener('keydown', key)
    }
  }, [open])
  const current = options.find(([v]) => v === value)?.[1] ?? value
  return (
    <span ref={root} className="sh-menu" data-component="MenuButton" onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) setOpen(false) }}>
      <button ref={trigger} type="button" className="sh-btn" aria-haspopup="menu" aria-expanded={open} aria-label={`${label}: ${current}`} onClick={() => setOpen((o) => !o)}>
        {current} <span aria-hidden="true">▾</span>
      </button>
      {open && (
        <span className="sh-menu-list" role="menu" aria-label={label}>
          {options.map(([v, text]) => (
            <button key={v} type="button" role="menuitemradio" aria-checked={v === value} onClick={() => { setOpen(false); trigger.current?.focus(); if (v !== value) onPick(v) }}>{text}</button>
          ))}
        </span>
      )}
    </span>
  )
}

/** How often the page reads today's agent figures and quota holds again. */
const AGENT_REFRESH_MS = 30_000

/** What each agent did today: what it spent (the daemon's total over all its runs) and the lanes it ran in (`more` when the list of runs was cut). */
interface AgentToday { lanes: number; usd: number; more: boolean }

function useAgentToday(workspaces: number[], agents: string[]) {
  const [rows, setRows] = useState<Map<string, AgentToday>>(new Map())
  const key = workspaces.join(',')
  const names = agents.join(',')
  useEffect(() => {
    if (!key) return
    let live = true
    const load = () => {
      const since = rangeStart('today', Date.now()).toISOString()
      // One read per agent: the totals are the daemon's, so a long day is not cut at the list's limit.
      void Promise.all(
        names.split(',').map((a) =>
          Promise.all(key.split(',').map((id) => history.runs(Number(id), since, a, 0))).then((parts) => {
            const h = mergeHistories(parts)
            const lanes = new Set((h.runs as RunRecord[]).flatMap((r) => (r.lane_id ? [r.lane_id] : [])))
            return [a, h.count === 0 ? null : { lanes: lanes.size, usd: h.cost_usd, more: h.count > h.runs.length }] as const
          }),
        ),
      ).then(
        (all) => {
          if (!live) return
          setRows(new Map(all.flatMap(([a, v]) => (v ? [[a, v] as const] : []))))
        },
        () => undefined,
      )
    }
    load()
    const t = window.setInterval(load, AGENT_REFRESH_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [key, names])
  return rows
}

/**
 * The diff's lines, each with the risk it carries. The YAML key path of every shown line is
 * tracked from its indentation, and an added line is risky only when that path is the risk's
 * full dotted path. A risk no shown line matches (a hunk without its parents) comes back
 * as `unplaced`, so it is still listed rather than pinned on a guess.
 */
function diffLines(diff: string, risky: ConfigRisk[]) {
  const claimed = new Set<number>()
  const stack: { indent: number; key: string }[] = []
  const lines = diff.split('\n').filter((l, i, all) => l !== '' || i < all.length - 1).map((text, i) => {
    const sign = text.startsWith('+') ? '+' : text.startsWith('-') ? '-' : text.startsWith('@@') ? '@' : ' '
    let risk: ConfigRisk | undefined
    if (sign === '@') stack.length = 0
    else if (sign !== '-') {
      const m = /^(\s*)(?:- )?([^\s:#][^:]*):(\s|$)/.exec(text.slice(1))
      if (m) {
        const indent = (m[1] ?? '').length
        while (stack.length > 0 && (stack[stack.length - 1]?.indent ?? 0) >= indent) stack.pop()
        const key = (m[2] ?? '').trim()
        const path = [...stack.map((x) => x.key), key].join('.')
        if (sign === '+') {
          const j = risky.findIndex((r, n) => !claimed.has(n) && r.path === path)
          if (j >= 0) {
            claimed.add(j)
            risk = risky[j]
          }
        }
        stack.push({ indent, key })
      }
    }
    return { i, sign, text: sign === '@' ? text : sign === ' ' ? text : text.slice(1), risk }
  })
  return { lines, unplaced: risky.filter((_, n) => !claimed.has(n)) }
}

function StagedBar({ preview, risky, problems, count, canWrite, why, busy, onDiscard, onSave }: {
  preview: ConfigPreview | null; risky: ConfigRisk[]; problems: string[]; count: number; canWrite: boolean; why?: string; busy: boolean
  onDiscard: () => void; onSave: () => void
}) {
  const { lines, unplaced } = preview ? diffLines(preview.diff, risky) : { lines: [], unplaced: [] }
  return (
    <section className="sh-staged" aria-label="Unsaved changes" data-component="StagedBar">
      <div className="sh-staged-inner">
        <div className="sh-staged-head">
          <strong>{count} unsaved change{count === 1 ? '' : 's'}</strong>
          {risky.length > 0 && <span className="sh-risk-tag"><span aria-hidden="true">◆</span> {risky.length} risky</span>}
          <span className="sh-setting-desc">Nothing changes until you save.</span>
          <span className="sh-staged-actions">
            <Button variant="ghost" onClick={onDiscard}>Discard</Button>
            {canWrite && (
              <Button variant="attention" onClick={onSave} disabled={busy || !preview || !preview.diff || problems.length > 0}>
                {busy ? 'Saving…' : risky.length > 0 ? 'Save…' : 'Save'}
              </Button>
            )}
          </span>
        </div>
        {!canWrite && <p className="sh-setting-desc">{why ?? 'This session cannot save settings.'} The diff is what a save would write.</p>}
        {problems.length > 0 && (
          <div className="sh-staged-problems" role="alert">
            <strong>Not saved: the result is not a valid config.</strong>
            <ul>{problems.map((p) => <li key={p}>{p}</li>)}</ul>
          </div>
        )}
        <div className="sh-diff" role="region" aria-label="Diff of config.yaml">
          <div className="sh-diff-file">config.yaml</div>
          {!preview && problems.length === 0 && <div className="sh-diff-line"><span /><span className="sh-setting-desc">Working out the diff…</span></div>}
          {preview && !preview.diff && problems.length === 0 && <div className="sh-diff-line"><span /><span className="sh-setting-desc">These changes leave the file as it is.</span></div>}
          {lines.map((l) => (
            <div key={l.i}>
              <div className={`sh-diff-line${l.sign === '+' ? ' is-add' : l.sign === '-' ? ' is-del' : ''}`}>
                <span>{l.sign === '@' ? '' : l.sign === ' ' ? '' : l.sign}</span>
                <span className={l.sign === '@' ? 'sh-diff-gap' : undefined}>{l.text}</span>
                {l.risk && <span className="sh-risk-flag"><span aria-hidden="true">◆</span> Risky</span>}
              </div>
              {l.risk && <div className="sh-diff-why">{l.risk.why}.</div>}
            </div>
          ))}
        </div>
        {unplaced.length > 0 && (
          <ul className="sh-diff-unplaced" aria-label="Risky changes not shown on a line">
            {unplaced.map((r) => (
              <li key={r.path}><span className="sh-risk-flag"><span aria-hidden="true">◆</span> Risky</span> <code>{r.path}</code>: {r.why}.</li>
            ))}
          </ul>
        )}
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
  const [asking, setAsking] = useState(false)
  const [busy, setBusy] = useState(false)
  const [notice, setNotice] = useState<{ tone: 'ok' | 'bad'; text: string } | null>(null)
  const [stale, setStale] = useState(false)
  const [added, setAdded] = useState<string[]>([])
  const [newRepo, setNewRepo] = useState('')
  const [addedProjects, setAddedProjects] = useState<string[]>([])
  const [newProject, setNewProject] = useState('')
  const [projectProblem, setProjectProblem] = useState<string | null>(null)
  const [theme, setTheme] = useState<Theme>(readTheme)
  const [forge, setForge] = useState<StatusDetail['forge'] | 'unknown' | undefined>(undefined)
  const [workspaces, setWorkspaces] = useState<{ id: number; name: string }[]>([])
  const [holds, setHolds] = useState<{ agent: string; until: string }[]>([])
  const projectBox = useRef<HTMLInputElement>(null)
  const now = useNow()
  const today = useAgentToday(workspaces.map((w) => w.id), AGENTS)

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
  useEffect(() => {
    const read = () =>
      daemonInfo.status().then(
        (s) => {
          setForge(s.detail?.forge ?? 'unknown')
          setWorkspaces((w) => (JSON.stringify(w) === JSON.stringify(s.workspaces ?? []) ? w : (s.workspaces ?? [])))
          setHolds(s.detail?.quota_holds ?? [])
        },
        () => setForge((f) => f ?? 'unknown'),
      )
    void read()
    const t = window.setInterval(() => void read(), AGENT_REFRESH_MS)
    return () => window.clearInterval(t)
  }, [])
  // "New project" on the Projects page lands here with the add box ready.
  const loaded = doc !== null
  useEffect(() => {
    if (scopeId === NEW_PROJECT && loaded) projectBox.current?.focus()
  }, [scopeId, loaded])

  const ops = useMemo(() => opsOf(draft), [draft])
  // The diff follows the staged edits. A preview writes nothing.
  useEffect(() => {
    if (!doc || ops.length === 0) return
    let live = true
    const t = setTimeout(() => {
      configApi.preview({ etag: doc.etag, ops }).then(
        (p) => {
          if (!live) return
          setPreview(p)
          setRisky(p.risky ?? [])
          setProblems([])
        },
        (e: unknown) => {
          if (!live) return
          const r = refusal(e)
          if (e instanceof ApiError && e.status === 409) setStale(true)
          else if (e instanceof ApiError && e.status === 422 && r?.problems) {
            setProblems(r.problems)
            setPreview({ etag: doc.etag, diff: '' })
            setRisky([])
          } else setNotice({ tone: 'bad', text: message(e) })
        },
      )
    }, 200)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [doc, ops])

  const gone = useMemo(() => deletedProjects(draft), [draft])
  const all = useMemo(
    () => (doc ? scopes(doc, added, addedProjects).filter((s) => !(s.id.startsWith('project:') && gone.has(s.id.slice('project:'.length)))) : []),
    [doc, added, addedProjects, gone],
  )
  const scope = pickScope(all, scopeId)

  if (loadError && !doc) return <div className="sh-settings"><p className="banner tone-bad" role="alert">Settings did not load: {loadError}</p></div>
  if (!doc || !scope) return <div className="sh-settings"><p className="sh-setting-desc">Loading settings…</p></div>

  const readOnly = !doc.can_write

  const onDraft = (d: Draft) => {
    setDraft(d)
    setPreview(null)
    setProblems([])
    setRisky([])
    setNotice(null)
  }

  // What is saved for a path, and what it reads as with the staged edits.
  const saved = (path: string[]): unknown => (path[0] === 'repos' && path.length > 2 ? getAt(doc.repos[path[1] ?? '']?.effective, path.slice(2)) : getAt(doc.effective, path))
  const staged = (path: string[]): unknown => {
    const op = ops.find((o) => dotted(o.path) === dotted(path))
    return op ? (op.unset ? saved(path) : op.value) : saved(path)
  }
  const stage = (path: string[], value: unknown) => onDraft(value === saved(path) ? revert(draft, path) : setValue(draft, path, value))
  const was = (path: string[], fmt: (v: unknown) => string = String): string | undefined => {
    const op = ops.find((o) => dotted(o.path) === dotted(path))
    return op ? fmt(saved(path)) : undefined
  }

  const attempt = async (confirm: string[]) => {
    setBusy(true)
    try {
      const out = await configApi.save({ etag: doc.etag, ops, confirm })
      setDraft({})
      setPreview(null)
      setRisky([])
      setAdded([])
      setAddedProjects([])
      setAsking(false)
      setNotice(out.reloaded ? { tone: 'ok', text: 'Saved and reloaded. Running agents keep what they started with.' } : { tone: 'bad', text: `Saved, but the daemon did not reload it: ${out.reload_error ?? 'unknown error'}` })
      load()
    } catch (e) {
      setAsking(false)
      const r = refusal(e)
      if (e instanceof ApiError && e.status === 409) {
        setStale(true)
        setPreview(null)
      } else if (e instanceof ApiError && e.status === 422 && r?.problems) setProblems(r.problems)
      else if (e instanceof ApiError && e.status === 428 && r?.risky) setRisky(r.risky)
      else setNotice({ tone: 'bad', text: message(e) })
    } finally {
      setBusy(false)
    }
  }
  const save = () => (risky.length > 0 ? setAsking(true) : attempt([]))

  const money = (n: number) => usd(n) || '$0.00'
  const num = (v: unknown) => (typeof v === 'number' ? v : 0)
  const budgetPath = ['daemon', 'budget']
  const capPath = ['keep_going', 'cap']
  const runsPath = ['daemon', 'max_runs']
  const polled = forge && forge !== 'unknown' && forge.last_poll ? since(forge.last_poll, now) : ''
  const pollEvery = String(getAt(doc.effective, ['daemon', 'poll']) ?? '')
  const models = (getAt(doc.defaults.effective, ['agent', 'model']) ?? {}) as Record<string, unknown>
  const projectOf = new Map<string, string>()
  for (const [p, v] of Object.entries(doc.projects)) for (const r of asList(v.effective.repos)) projectOf.set(r, p)

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
        {heading && <h3>{heading}</h3>}
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
    <div className="sh-settings" data-component="Settings">
      <header className="sh-settings-head">
        <h1>Settings</h1>
        <span className="sh-settings-meta sh-setting-desc" title={`Editing ${doc.path}${doc.missing ? ' (not written yet)' : ''}`}>
          {workspaces[0] && <>Workspace {workspaces.length === 1 ? workspaces[0].name : `${workspaces.length} workspaces`} · </>}
          Changes apply when you press Save · <a href={href.status()}>Status →</a>
        </span>
      </header>
      <div className="sh-settings-body">
        <div className="sh-section">
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
              <Button size="small" onClick={load}>Reload it</Button>, then stage your changes again.
            </p>
          )}
          {notice && <p className={`banner tone-${notice.tone === 'ok' ? 'ok' : 'bad'}`} role="status">{notice.text}</p>}

          <h2>Budget</h2>
          <SettingRow name="budget" label="Daily cap" desc="Past it Shepherd holds the runs it would start on its own. 0 is no cap." was={was(budgetPath, (v) => money(num(v)))}>
            <Stepper label="daily cap" value={num(staged(budgetPath))} step={5} min={0} format={money} onChange={(n) => stage(budgetPath, n)} />
          </SettingRow>
          <SettingRow name="keep_going" label="Keep going" desc="What it may spend once turned on; at the cap it turns itself off. Turn it on from the sidebar." was={was(capPath, (v) => money(num(v)))}>
            <Stepper label="keep going cap" value={num(staged(capPath))} step={1} min={0} format={money} onChange={(n) => stage(capPath, n)} />
          </SettingRow>
          <SettingRow name="max_runs" label="Parallel lanes" desc="How many agents may run at once on this machine." was={was(runsPath)}>
            <Stepper label="parallel lanes" value={num(staged(runsPath))} step={1} min={1} format={String} onChange={(n) => stage(runsPath, n)} />
          </SettingRow>

          <h2>Agents</h2>
          {AGENTS.map((a) => {
            const m = models[a]
            return (
              <div key={a} className="sh-setting" data-component="SettingRow" data-setting={`agent-${a}`}>
                <div>
                  <div className="sh-setting-label">
                    <b>{a}</b>{' '}
                    {typeof m === 'string' && m ? <code className="sh-setting-model" title={m}>{bareModel(m)}</code> : null}
                  </div>
                  <AgentLine agent={a} model={typeof m === 'string' && m ? m : ''} today={today.get(a)} hold={holds.find((h) => h.agent === a)} now={now} />
                </div>
                <a className="sh-btn" href={href.settings('defaults')} aria-label={`Configure ${a}`}>Configure</a>
              </div>
            )
          })}

          <h2>Forge</h2>
          <SettingRow name="forge" label="Forge watcher" desc={
            forge === undefined ? 'Reading the poll status…' : forge === 'unknown' ? 'Poll status is not available.' : forge.error ? (
              <><StatusGlyph glyph="✗" tone="bad" label="Broken" /> {forge.error}</>
            ) : (
              <><StatusGlyph glyph={polled ? '●' : '○'} tone={polled ? 'ok' : 'idle'} label={polled ? 'Polling' : 'Not polled yet'} /> {polled ? `Polled ${polled} ago` : 'Not polled yet'} · every {pollEvery || 'unset'}</>
            )
          }>
            <a className="sh-btn" href={href.settings('daemon')}>Configure</a>
          </SettingRow>

          <h2>Merging</h2>
          {Object.keys(doc.repos).sort().map((r) => {
            const path = ['repos', r, 'autonomy', 'merge']
            const v = String(staged(path) ?? 'human')
            const person = v !== 'agent'
            return (
              <SettingRow
                key={r}
                name={`merge-${r}`}
                label={<><code>{r}</code> {projectOf.has(r) && <span className="sh-setting-desc">in {projectOf.get(r)}</span>}</>}
                desc={person ? 'A person approves each merge request.' : 'An agent arms merge when the pipeline passes; the forge still gates it.'}
                was={was(path, (x) => (x === 'agent' ? 'By an agent' : 'By a person'))}
              >
                <Menu label={`Merging for ${r}`} value={person ? 'human' : 'agent'} options={[['human', 'By a person'], ['agent', 'By an agent']]} onPick={(x) => stage(path, x)} />
              </SettingRow>
            )
          })}

          <h2>Appearance</h2>
          <SettingRow name="theme" label="Theme" desc="This browser only. Applies now, outside Save.">
            <Segmented label="Theme" value={theme} options={THEMES} onPick={(t) => {
              setTheme(t)
              applyTheme(t)
              try {
                window.localStorage.setItem(THEME_KEY, t)
              } catch {
                /* a browser that will not remember keeps the choice for this page only */
              }
            }} />
          </SettingRow>

          <h2>Keyboard</h2>
          <div className="sh-keyref">
            {KEYS.map(([what, keys]) => (
              <div key={what}><span>{what}</span><span><Kbd keys={keys} /></span></div>
            ))}
          </div>

          <details className="sh-advanced" open={scopeId !== undefined}>
            <summary>All settings</summary>
            <div className="sh-advanced-layout">
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
                <h3>{scope.section.label}{scope.id.includes(':') ? `: ${scope.label}` : ''}</h3>
                {scope.section.fields.length === 0 && <p className="sh-setting-desc">Nothing to set here.</p>}
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
              </div>
            </div>
          </details>
        </div>
      </div>
      {ops.length > 0 && (
        <StagedBar preview={preview} risky={risky} problems={problems} count={ops.length} canWrite={doc.can_write} why={doc.why} busy={busy}
          onDiscard={() => onDraft({})} onSave={save} />
      )}
      <Confirm
        open={asking}
        title="Save changes that give agents more power?"
        confirmLabel="Save and reload"
        tone="warn"
        onCancel={() => setAsking(false)}
        onConfirm={() => attempt(risky.map((r) => r.path))}
        items={risky.map((r) => (
          <><code>{r.path}</code>: {r.from || '(empty)'} → {r.to || '(empty)'}. {r.why}.</>
        ))}
      />
    </div>
  )
}
