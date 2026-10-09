import { useEffect, useRef, useState } from 'react'
import { GROUP_NAMES, GROUPS, type BoardItem, type Group } from '../model/board'
import { GROUP_MARKS } from '../model/groups'
import { Glyph } from '../components/Glyph'
import { MrBadge } from '../components/MrBadge'
import { since } from '../format'
import { useKeys, useNow } from '../hooks'
import { go, href } from '../router'
import { useLive, useSnapshot } from '../state/context'

/** Where a row leads: a decision to its answer, a lane to its page. */
function target(it: BoardItem): string | null {
  if (it.decision) return href.decisions(it.decision.id)
  if (it.laneId !== undefined) return href.lane(it.laneId)
  return null
}

export function BoardPage({ items }: { items: BoardItem[] }) {
  const snap = useSnapshot()
  const live = useLive()
  const now = useNow()
  const [sel, setSel] = useState(0)
  const rows = useRef<(HTMLAnchorElement | HTMLDivElement | null)[]>([])

  const clamped = Math.min(sel, Math.max(0, items.length - 1))
  useEffect(() => {
    rows.current[clamped]?.focus({ preventScroll: false })
  }, [clamped])

  useKeys(
    {
      j: () => setSel((s) => Math.min(s + 1, items.length - 1)),
      ArrowDown: (e) => (e.preventDefault(), setSel((s) => Math.min(s + 1, items.length - 1))),
      k: () => setSel((s) => Math.max(s - 1, 0)),
      ArrowUp: (e) => (e.preventDefault(), setSel((s) => Math.max(s - 1, 0))),
      c: () => live.clearDone(),
    },
    [items.length, live],
  )

  if (!snap.ready) {
    return <p className="empty">{snap.error ? 'Waiting for the daemon.' : 'Loading lanes…'}</p>
  }
  if (items.length === 0) {
    return (
      <div className="empty">
        <p>Nothing needs you, nothing is running, and nothing finished since you last looked.</p>
        <p className="faint">
          Start work from the terminal: <code>shepherd lane run &lt;lane&gt; --agent claude</code>.
        </p>
      </div>
    )
  }

  const byGroup = new Map<Group, BoardItem[]>()
  for (const it of items) byGroup.set(it.group, [...(byGroup.get(it.group) ?? []), it])
  let index = -1

  return (
    <div className="board">
      {GROUPS.flatMap((g) => {
        const list = byGroup.get(g)
        return list ? [{ g, list }] : []
      }).map(({ g, list }) => (
        <section key={g} className={`group group-${g}`} aria-labelledby={`group-${g}`}>
          <h2 id={`group-${g}`} className="group-head">
            <Glyph glyph={GROUP_MARKS[g].glyph} tone={GROUP_MARKS[g].tone} label="" />
            <span className="group-name">{GROUP_NAMES[g]}</span>
            <span className="group-count">{list.length}</span>
            {g === 'done' && (
              <button type="button" className="link-button" onClick={() => live.clearDone()}>
                Mark all seen <kbd>c</kbd>
              </button>
            )}
          </h2>
          <ul className="rows">
            {list.map((it) => {
              const i = ++index
              const to = target(it)
              const mark = GROUP_MARKS[it.group]
              const body = (
                <>
                  <Glyph glyph={it.glyph ?? mark.glyph} tone={mark.tone} label={GROUP_NAMES[it.group]} />
                  <span className="row-lane">{it.name}</span>
                  <span className="row-agent">{it.agent ?? ''}</span>
                  <span className="row-repo">{it.repo}</span>
                  <span className="row-mr">{it.lane && <MrBadge lane={it.lane} />}</span>
                  <span className="row-detail">{it.detail}</span>
                  <span className="row-age" title={it.at}>
                    {since(it.at, now)}
                  </span>
                </>
              )
              return (
                <li key={it.key}>
                  {to ? (
                    <a
                      ref={(el) => {
                        rows.current[i] = el
                      }}
                      className="row"
                      href={to}
                      aria-current={i === clamped ? 'true' : undefined}
                      onFocus={() => setSel(i)}
                      onClick={(e) => {
                        e.preventDefault()
                        go(to)
                      }}
                    >
                      {body}
                    </a>
                  ) : (
                    <div
                      ref={(el) => {
                        rows.current[i] = el
                      }}
                      className="row"
                      tabIndex={0}
                      onFocus={() => setSel(i)}
                    >
                      {body}
                    </div>
                  )}
                </li>
              )
            })}
          </ul>
        </section>
      ))}
    </div>
  )
}
