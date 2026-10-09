import { EVENTS, MARKS } from '../model/marks'
import { Glyph } from './Glyph'
import '../styles/legend.css'

/** A legend for feed events. */
export function Legend() {
  return (
    <dl className="legend">
      {EVENTS.map(event => {
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