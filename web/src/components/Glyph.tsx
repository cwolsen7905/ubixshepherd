import { markFor, type Tone } from '../model/marks'
import { cx } from '../ui/cx'
import { glyphTone, StatusGlyph } from '../ui/StatusGlyph'

/** An event's glyph in its tone; the label is read out in its place. */
export function EventGlyph({ event }: { event: string }) {
  const m = markFor(event)
  return <Glyph glyph={m.glyph} tone={m.tone} label={m.label} />
}

/**
 * A feed glyph in its tone: the shared StatusGlyph, in the gutter column the older pages lay
 * out (`glyph` is the hook their grids select).
 */
export function Glyph({ glyph, tone, label }: { glyph: string; tone: Tone; label: string }) {
  return <StatusGlyph glyph={glyph} tone={glyphTone(tone)} label={label} className={cx('glyph', `tone-${tone}`)} />
}
