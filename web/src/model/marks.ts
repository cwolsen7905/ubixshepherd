// How each feed event is drawn: a glyph that carries the meaning alone, so the timeline
// reads the same without colour, and a tone. The same table as the terminal's
// (internal/chat/events.go); the set of events is internal/api/events.go's.

/** Every event the daemon sends, in the order events.go lists them. */
export const EVENTS = ['info'] as const

export type EventKind = (typeof EVENTS)[number]

export type Tone = 'muted' | 'accent' | 'warn' | 'ok' | 'bad' | 'broken'

export interface Mark {
  glyph: string
  tone: Tone
  /** What the glyph means, for screen readers and the legend: "run passed". */
  label: string
}

export const MARKS: Record<EventKind, Mark> = { info: { glyph: '·', tone: 'muted', label: 'note' } }

/** The mark for any event string; one this table does not know reads as info. */
export function markFor(event: string): Mark {
  void event
  throw new Error('not implemented')
}
