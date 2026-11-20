import { GLYPHS, KEYS } from './keys'

// The source files are read as text: bindings still live in hooks.ts's useKeys callers and in
// onKeyDown handlers, so this scans for them. It is tolerant on purpose: it finds the common
// patterns, and LOCAL_ONLY names keys that are bound for one widget and not worth a row.
const files = import.meta.glob('../**/*.{ts,tsx}', { query: '?raw', import: 'default', eager: true }) as Record<string, string>

const LOCAL_ONLY = new Set([
  'Tab', // focus trapping in dialogs and the status popovers
  'ArrowLeft', 'ArrowRight', 'Home', 'End', // tab lists and the divider
  ' ', // a list row also activates on Space
])

const KEY = String.raw`[A-Za-z0-9/\]\[?]|Escape|Enter|Arrow(?:Up|Down|Left|Right)|Home|End|Tab`

/** The key names the source binds, found by the useKeys object-literal and e.key patterns. */
function bound(): Set<string> {
  const found = new Set<string>()
  for (const [path, src] of Object.entries(files)) {
    if (/\.test\.tsx?$/.test(path) || path.includes('/test/') || path.endsWith('/lib/keys.ts')) continue
    if (src.includes('useKeys(')) {
      for (const m of src.matchAll(new RegExp(String.raw`(?:^|[{,])\s*(?:'(${KEY})'|(${KEY})):\s*\(`, 'gm'))) found.add(m[1] ?? m[2] ?? '')
    }
    for (const m of src.matchAll(/\b(?:e|ev|event)\.key\s*(?:===|!==)\s*'([^']+)'/g)) found.add(m[1] ?? '')
    // Number keys are built from the option index, not written out.
    if (src.includes('String(i + 1)')) '123456789'.split('').forEach((d) => found.add(d))
  }
  return found
}

describe('keys list', () => {
  const listed = new Set(KEYS.flatMap((k) => k.bound))
  const source = bound()

  it('finds bindings in the source, so the scan is not vacuous', () => {
    for (const k of ['j', ']', 'Escape', '1', '?']) expect(source.has(k)).toBe(true)
  })

  it('lists every key the source binds', () => {
    const missing = [...source].filter((k) => !listed.has(k) && !LOCAL_ONLY.has(k))
    expect(missing).toEqual([])
  })

  it('binds every key it lists', () => {
    const unbound = [...listed].filter((k) => !source.has(k))
    expect(unbound).toEqual([])
  })

  it('has no key listed twice with the same combo', () => {
    const combos = KEYS.flatMap((k) => k.combos.map((c) => c.join(' ')))
    expect(new Set(combos).size).toBe(combos.length)
  })
})

describe('glyph list', () => {
  it('matches the UI rules: ten glyphs, each with words', () => {
    expect(GLYPHS.map((g) => g.glyph).join('')).toBe('?!◆≡✗✓●○▸▤')
    for (const g of GLYPHS) expect(g.label.length).toBeGreaterThan(0)
  })
})
