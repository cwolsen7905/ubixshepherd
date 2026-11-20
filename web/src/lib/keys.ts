// One list of the keyboard shortcuts and status glyphs the UI documents. The keys sheet (Help)
// and the glyph legend (Legend) both render from it, and keys.test.ts checks it against the
// keys the source binds. Bindings still live where they are used (hooks.ts's useKeys and the
// components that call it); moving them onto this list is a follow-up.

import type { Tone } from '../model/marks'

export interface KeyEntry {
  /** The ways to press it; each is a sequence of keys shown one `kbd` apiece: `['g', 'b']`. */
  combos: string[][]
  /** What it does, in sentence case. */
  what: string
  /**
   * The `KeyboardEvent.key` values the source binds for it, which keys.test.ts looks for.
   * Chords with a modifier list the base key.
   */
  bound: string[]
}

export const KEYS: KeyEntry[] = [
  { combos: [['Ctrl+K'], ['⌘K']], what: 'Search or act: go to a page, open a lane, run or decision', bound: ['k'] },
  { combos: [['j'], ['k']], what: 'Move down and up a list', bound: ['j', 'k', 'ArrowDown', 'ArrowUp'] },
  { combos: [['Enter']], what: 'Open the lane or decision', bound: ['Enter'] },
  { combos: [[']']], what: 'Jump to the next item waiting on you', bound: [']'] },
  { combos: [['[']], what: 'Step back to the previous item', bound: ['['] },
  { combos: [['1 to 9']], what: 'Pick that option of the focused decision, without sending it', bound: ['1', '2', '3', '4', '5', '6', '7', '8', '9'] },
  { combos: [['⌘Enter'], ['Ctrl+Enter']], what: 'Send, then move on to the next item', bound: ['Enter'] },
  { combos: [['Esc']], what: 'Close the open panel, dialog or this sheet', bound: ['Escape'] },
  { combos: [['g', 'b']], what: 'Go to the board', bound: ['g', 'b'] },
  { combos: [['g', 'd']], what: 'Go to decisions', bound: ['g', 'd'] },
  { combos: [['g', 't']], what: 'Go to timeline', bound: ['g', 't'] },
  { combos: [['g', 'c']], what: 'Go to chat', bound: ['g', 'c'] },
  { combos: [['g', 'n']], what: 'Go to next up', bound: ['g', 'n'] },
  { combos: [['g', 'a']], what: 'Go to artifacts', bound: ['g', 'a'] },
  { combos: [['c']], what: 'On the board, mark everything done as seen', bound: ['c'] },
  { combos: [['/']], what: 'In a run log, search it', bound: ['/'] },
  { combos: [['f']], what: 'In a run log, follow it as it grows', bound: ['f'] },
  { combos: [['t']], what: 'In a run log, open or close every step', bound: ['t'] },
  { combos: [['n'], ['N']], what: 'In a run log, go to the next or previous match', bound: ['n', 'N'] },
  { combos: [['?']], what: 'Show or hide these keys', bound: ['?'] },
]

export interface GlyphEntry {
  glyph: string
  tone: Tone
  /** The words that always go with the glyph. */
  label: string
}

/** The status glyphs, as the UI rules fix them: each is a glyph with words, never colour alone. */
export const GLYPHS: GlyphEntry[] = [
  { glyph: '?', tone: 'warn', label: 'decision' },
  { glyph: '!', tone: 'warn', label: 'request' },
  { glyph: '◆', tone: 'warn', label: 'approve' },
  { glyph: '≡', tone: 'warn', label: 'next up' },
  { glyph: '✗', tone: 'broken', label: 'broken' },
  { glyph: '✓', tone: 'ok', label: 'ok' },
  { glyph: '●', tone: 'accent', label: 'working' },
  { glyph: '○', tone: 'muted', label: 'idle' },
  { glyph: '▸', tone: 'accent', label: 'run or tool call' },
  { glyph: '▤', tone: 'muted', label: 'artifact' },
]
