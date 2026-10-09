import { markFor, type Tone } from '../model/marks'

/** An event's glyph in its tone; the label is read out in its place. */
export function EventGlyph({ event }: { event: string }) {
  const m = markFor(event)
  return <Glyph glyph={m.glyph} tone={m.tone} label={m.label} />
}

export function Glyph({ glyph, tone, label }: { glyph: string; tone: Tone; label: string }) {
  return (
    <span className={`glyph tone-${tone}`} title={label}>
      <span aria-hidden="true">{glyph}</span>
      <span className="visually-hidden">{label}</span>
    </span>
  )
}
