import { cx } from './cx'

/** The tones a glyph is drawn in; `Tone` in model/marks maps onto these. */
export type GlyphTone = 'warn' | 'bad' | 'ok' | 'live' | 'idle' | 'held'

const FROM_MARK: Record<string, GlyphTone> = { warn: 'warn', bad: 'bad', broken: 'bad', ok: 'ok', accent: 'live', muted: 'held' }

/** The glyph tone for one of the feed's mark tones. */
export function glyphTone(tone: string): GlyphTone {
  return FROM_MARK[tone] ?? 'idle'
}

/**
 * A status glyph (`.sh-glyph`) in its tone. Always paired with words: `label` is read out in its
 * place and shown as the tooltip. Without `label` the glyph is decoration beside text.
 */
export function StatusGlyph({ glyph, tone = 'idle', label, className }: { glyph: string; tone?: GlyphTone; label?: string; className?: string }) {
  return (
    <span className={cx('sh-glyph', `sh-glyph--${tone}`, className)} data-component="StatusGlyph" title={label}>
      <span aria-hidden="true">{glyph}</span>
      {label && <span className="visually-hidden">{label}</span>}
    </span>
  )
}
