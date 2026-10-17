import { EVENTS, MARKS } from '../model/marks'
import { Glyph } from './Glyph'

// The legend of the feed's glyphs, for the keys sheet: every event's glyph in its tone
// and what it means, so a glyph can be learned without relying on its colour.

/** A list of every mark in MARKS, in EVENTS order, in two auto-fill columns. */
export function Legend() {
  return (
    <dl className="sh-glyphs" data-component="Legend">
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
