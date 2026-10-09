import type { Tone } from './marks'
import type { Group } from './board'

/** The board's glyph per group, as the dock's groupMarks. */
export const GROUP_MARKS: Record<Group, { glyph: string; tone: Tone }> = {
  needs: { glyph: '?', tone: 'warn' },
  broken: { glyph: '✗', tone: 'broken' },
  review: { glyph: '◆', tone: 'accent' },
  working: { glyph: '●', tone: 'accent' },
  done: { glyph: '✓', tone: 'ok' },
}
