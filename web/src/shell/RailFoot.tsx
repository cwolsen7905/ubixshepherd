import { useCallback, useEffect, useRef, useState, type ReactNode } from 'react'
import { get, post } from '../api/client'
import { KeepGoing, REFRESH_MS, type KeepGoingStatus } from '../components/KeepGoing'
import { Icon, StatusGlyph, cx, useOffline, type GlyphTone } from '../ui'
import { Flyout } from './Flyout'

const KEEP_PATH = '/v1/keep-going'

/** The one status the collapsed footer shows: a glyph, its tone, and the words it stands for. */
export interface RailStatus {
  glyph: string
  tone: GlyphTone
  words: string
}

/** Offline or failing is ✗, then ● with how many lanes are working, else ✓ online. */
export function railStatus({ offline, running, broken }: { offline: boolean; running: number; broken: number }): RailStatus {
  if (offline) return { glyph: '✗', tone: 'bad', words: 'Offline' }
  if (broken > 0) return { glyph: '✗', tone: 'bad', words: `${broken} failing` }
  if (running > 0) return { glyph: '●', tone: 'live', words: `${running} working` }
  return { glyph: '✓', tone: 'ok', words: 'Online' }
}

/**
 * Keep going as a small switch for the rail, lit while on. It reads and sets the same daemon
 * route as the full switch; turning it on is still the person's click and nothing else. A failed
 * read shows nothing, as the full switch does.
 */
export function KeepGoingIcon({
  load = () => get<KeepGoingStatus>(KEEP_PATH),
  set = (on) => post<KeepGoingStatus>(KEEP_PATH, { on }),
}: {
  load?: () => Promise<KeepGoingStatus>
  set?: (on: boolean) => Promise<KeepGoingStatus>
}) {
  const [st, setSt] = useState<KeepGoingStatus | null>(null)
  const [busy, setBusy] = useState(false)
  useEffect(() => {
    let live = true
    const read = () => load().then((s) => live && setSt(s), () => undefined)
    void read()
    const t = window.setInterval(() => void read(), REFRESH_MS)
    return () => {
      live = false
      window.clearInterval(t)
    }
  }, [load])
  if (!st) return null
  const words = st.on ? 'Keep going, on' : 'Keep going, off'
  return (
    <button
      type="button"
      role="switch"
      aria-checked={st.on}
      aria-label="Keep going"
      title={words}
      disabled={busy}
      className={cx('sh-rail-btn', st.on && 'is-on')}
      data-component="KeepGoingIcon"
      onClick={() => {
        setBusy(true)
        set(!st.on)
          .then(setSt, () => undefined)
          .finally(() => setBusy(false))
      }}
    >
      <Icon name="keep-going" />
    </button>
  )
}

/**
 * The footer collapsed to the rail: Keep going as a small switch, then one status glyph with its
 * words in its name. Clicking the glyph opens the full footer in a flyout beside the rail; Esc
 * or a click elsewhere closes it and gives focus back to the glyph.
 */
export function RailFoot({ running, broken, children }: { running: number; broken: number; children: ReactNode }) {
  const { offline } = useOffline()
  const s = railStatus({ offline, running, broken })
  const [open, setOpen] = useState(false)
  const [anchor, setAnchor] = useState<HTMLButtonElement | null>(null)
  const btn = useRef<HTMLButtonElement | null>(null)
  const pop = useRef<HTMLDivElement>(null)
  const close = useCallback((refocus: boolean) => {
    setOpen(false)
    if (refocus) btn.current?.focus()
  }, [])

  useEffect(() => {
    if (!open) return
    const first = window.setTimeout(() => pop.current?.focus(), 0)
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') close(true)
    }
    // A count's own card is drawn in the body too, so any flyout counts as inside.
    const away = (e: MouseEvent) => {
      const t = e.target as Element | null
      if (btn.current?.contains(t) || t?.closest?.('.sh-flyout')) return
      close(false)
    }
    document.addEventListener('keydown', onKey)
    document.addEventListener('mousedown', away)
    return () => {
      window.clearTimeout(first)
      document.removeEventListener('keydown', onKey)
      document.removeEventListener('mousedown', away)
    }
  }, [open, close])

  return (
    <div className="sh-rail-foot" data-component="RailFoot">
      <KeepGoingIcon />
      <button
        ref={(el) => {
          btn.current = el
          setAnchor(el)
        }}
        type="button"
        className="sh-rail-btn sh-rail-status"
        aria-label={`${s.words}, show the footer`}
        aria-expanded={open}
        aria-haspopup="dialog"
        title={s.words}
        data-status={s.tone}
        onClick={() => setOpen((o) => !o)}
      >
        <StatusGlyph glyph={s.glyph} tone={s.tone} />
      </button>
      {open && (
        <Flyout ref={pop} anchor={anchor} className="sh-rail-pop" role="dialog" aria-label="Footer" tabIndex={-1}>
          <div className="sh-rail-pop-card sh-side-foot">
            <KeepGoing />
            {children}
          </div>
        </Flyout>
      )}
    </div>
  )
}
