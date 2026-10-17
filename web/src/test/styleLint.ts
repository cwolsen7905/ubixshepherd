// The style lint (MAINT-C-03): no hex colour, no px font size and no font family in a
// component or page's CSS, TS or TSX. Those belong to the design's tokens (styles/tokens.css) and
// its component classes (styles/components.css), which are the two files exempt from it.
// Older files that still carry some are counted in styleAllowlist.json: a count may only go down.

export type Rule = 'hex' | 'px-font' | 'font-family'

export const RULES: readonly Rule[] = ['hex', 'px-font', 'font-family']

export interface Finding {
  rule: Rule
  line: number
  text: string
}

/** Files under src that are the source of the values and so exempt. */
export const EXEMPT = ['src/styles/tokens.css', 'src/styles/components.css']

const HEX = String.raw`#(?:[0-9a-fA-F]{8}|[0-9a-fA-F]{6}|[0-9a-fA-F]{3,4})(?![\w-])`
const GENERIC = /\b(?:sans-serif|serif|monospace|system-ui|ui-monospace|ui-sans-serif|Arial|Helvetica|Menlo|Consolas|Georgia|Times)\b/i

/** Blanks out a span but keeps its newlines, so line numbers stay right. */
const blank = (s: string) => s.replace(/[^\n]/g, ' ')

/** CSS with comments, `@font-face` blocks (they define the families) and `url()` removed. */
function cleanCss(text: string): string {
  return text
    .replace(/\/\*[\s\S]*?\*\//g, blank)
    .replace(/@font-face\s*{[^}]*}/g, blank)
    .replace(/url\([^)]*\)/g, blank)
}

function inspect(line: string, css: boolean): Rule[] {
  const found: Rule[] = []
  const hex = css ? new RegExp(String.raw`(?<![\w&])${HEX}`) : new RegExp(String.raw`(?<=['"\x60\s(,:])${HEX}`)
  if (hex.test(line) && !(!css && /href|#\//.test(line))) found.push('hex')
  if (/(?<![\w-])font-size\s*:[^;}]*\d(?:\.\d+)?px/.test(line) || /(?<![\w-])font\s*:[^;}]*\d(?:\.\d+)?px/.test(line)) found.push('px-font')
  if (!css && /\bfontSize\s*:\s*['"\x60]?\d/.test(line)) found.push('px-font')
  const noVars = line.replace(/var\([^)]*\)/g, '')
  const family = /(?<![\w-])font-family\s*:([^;}]*)/.exec(noVars)?.[1]?.trim()
  if (family && !/^(?:inherit|initial|unset|revert)$/.test(family)) found.push('font-family')
  else if (/(?<![\w-])font\s*:/.test(noVars) && (/["']/.test(noVars) || GENERIC.test(noVars))) found.push('font-family')
  if (!css && /\bfontFamily\b/.test(line)) found.push('font-family')
  return found
}

/** Every violation in one file's text; `file` decides CSS or script rules by its extension. */
export function lintText(file: string, text: string): Finding[] {
  const css = file.endsWith('.css')
  const body = css ? cleanCss(text) : text
  const out: Finding[] = []
  body.split('\n').forEach((line, i) => {
    for (const rule of inspect(line, css)) out.push({ rule, line: i + 1, text: line.trim().slice(0, 120) })
  })
  return out
}

export type Counts = Partial<Record<Rule, number>>
export type Allowlist = Record<string, Counts>

/** Counts per rule for a file's findings. */
export function count(findings: Finding[]): Counts {
  const c: Counts = {}
  for (const f of findings) c[f.rule] = (c[f.rule] ?? 0) + 1
  return c
}

/** The findings that go past what the allowlist grants the file. */
export function overAllowance(file: string, findings: Finding[], allow: Allowlist): Finding[] {
  const granted = allow[file] ?? {}
  const seen: Counts = {}
  return findings.filter((f) => {
    const n = (seen[f.rule] ?? 0) + 1
    seen[f.rule] = n
    return n > (granted[f.rule] ?? 0)
  })
}
