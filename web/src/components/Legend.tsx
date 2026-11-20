import { GLYPHS } from '../lib/keys'
import { EVENTS, MARKS } from '../model/marks'
import { Glyph } from './Glyph'

// The legend: the status glyphs from lib/keys, then the feed's glyphs, for the keys sheet: every event's glyph in its tone
// and what it means, so a glyph can be learned without relying on its colour.

/** The status glyphs, then every mark in MARKS, in EVENTS order, in two auto-fill columns. */
export function Legend() {
  return (
    <dl className="sh-glyphs" data-component="Legend">
      {GLYPHS.map((g) => (
        <div key={`status-${g.glyph}`}>
          <dt>
            <Glyph glyph={g.glyph} tone={g.tone} label={g.label} />
          </dt>
          <dd>{g.label}</dd>
        </div>
      ))}
      {EVENTS.map((event) => {
        const m = MARKS[event]
        return (
          <div key={event}>
            <dt>
              <Glyph glyph={m.glyph} tone={m.tone} label={m.label} />
            </dt>
            <dd>{m.label}</dd>
          </div>
        )
      })}
    </dl>
  )
}
