import { href } from '../router'
import type { StatusCounts as Counts } from '../state/attention'
import './StatusCounts.css'

const CHIPS = [
  { key: 'running', glyph: '●', word: 'running', name: 'running' },
  { key: 'waiting', glyph: '?', word: 'waiting on you', name: 'waiting on you' },
  { key: 'broken', glyph: '✗', word: 'broken or blocked', name: 'broken or blocked' },
  { key: 'routing', glyph: '!', word: 'needs routing', name: 'needing routing' },
] as const

/** Opens the Waiting on you panel: its toggle is the one control that owns that state. */
function openWaiting() {
  document.querySelector<HTMLButtonElement>('.waiting-toggle')?.click()
}

/**
 * The page head's counts: running, waiting on you, broken or blocked, needs routing. All
 * four always show; a zero is muted and keeps its place. One status region, announced when
 * the numbers change.
 */
export function StatusCounts({ counts }: { counts: Counts }) {
  return (
    <div className="status-counts" role="status" aria-label="Status counts">
      {CHIPS.map((c) => {
        const n = counts[c.key]
        const cls = `status-chip status-${c.key}${n === 0 ? ' status-zero' : ''}`
        const label = `${n} ${c.name}`
        const body = (
          <>
            <span className="status-glyph" aria-hidden="true">{c.glyph}</span>
            <span className="status-n">{n}</span>
            <span className="status-word"> {c.word}</span>
          </>
        )
        if (c.key === 'waiting') {
          return (
            <button key={c.key} type="button" className={cls} aria-label={label} onClick={openWaiting}>
              {body}
            </button>
          )
        }
        const to = c.key === 'routing' ? href.decisions() : href.board()
        return (
          <a key={c.key} className={cls} href={to} aria-label={label}>
            {body}
          </a>
        )
      })}
    </div>
  )
}
