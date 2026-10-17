/// <reference types="node" />
import { readdirSync, readFileSync, statSync, writeFileSync } from 'node:fs'
import { join, relative } from 'node:path'
import { count, EXEMPT, lintText, overAllowance, type Allowlist, type Finding } from './styleLint'

const HERE = import.meta.dirname
const WEB = join(HERE, '..', '..')
const allowlist: Allowlist = JSON.parse(readFileSync(join(HERE, 'styleAllowlist.json'), 'utf8'))
const SRC = join(WEB, 'src')

function sources(dir: string): string[] {
  return readdirSync(dir).flatMap((name: string) => {
    const path = join(dir, name)
    if (statSync(path).isDirectory()) return name === 'test' ? [] : sources(path)
    return /\.(css|tsx?)$/.test(name) && !/\.test\.tsx?$/.test(name) ? [path] : []
  })
}

function scan(): Record<string, Finding[]> {
  const found: Record<string, Finding[]> = {}
  for (const path of sources(SRC)) {
    const file = relative(WEB, path)
    if (EXEMPT.includes(file)) continue
    const findings = lintText(file, readFileSync(path, 'utf8'))
    if (findings.length) found[file] = findings
  }
  return found
}

describe('style lint: detector', () => {
  const rules = (file: string, text: string) => lintText(file, text).map((f) => f.rule)

  it('finds a hex colour in CSS, in any of its lengths, and not in a comment or a url', () => {
    expect(rules('a.css', '.a { color: #fff; }')).toEqual(['hex'])
    expect(rules('a.css', '.a { background: #1c2630; border: 1px solid #abcd; }')).toEqual(['hex'])
    expect(rules('a.css', '.a { color: var(--sh-ink); } /* was #fff */')).toEqual([])
    expect(rules('a.css', '.a { background: url(a.svg#frag); }')).toEqual([])
    expect(rules('a.css', '#main:focus { outline: none; }')).toEqual([])
  })

  it('finds a hex colour in a string of a script, but not a link', () => {
    expect(rules('A.tsx', `const c = '#0f62fe'`)).toEqual(['hex'])
    expect(rules('A.tsx', `<rect fill="#ffffff" />`)).toEqual(['hex'])
    expect(rules('A.tsx', `<a href="#/board">`)).toEqual([])
    expect(rules('A.tsx', `go('#/board')`)).toEqual([])
  })

  it('finds a px font size, as font-size, in the font shorthand and in a style object', () => {
    expect(rules('a.css', '.a { font-size: 12px; }')).toEqual(['px-font'])
    expect(rules('a.css', '.a { font: 600 11.5px/16px var(--sh-font); }')).toEqual(['px-font'])
    expect(rules('a.css', '.a { font-size: var(--sh-t-meta); line-height: 16px; }')).toEqual([])
    expect(rules('a.css', '.a { font-size: 0.9rem; }')).toEqual([])
    expect(rules('A.tsx', `<b style={{ fontSize: 12 }} />`)).toEqual(['px-font'])
    expect(rules('A.tsx', `<b style={{ fontSize: '12px' }} />`)).toEqual(['px-font'])
  })

  it('finds a font family, but not a token, inherit, or the font-face that defines one', () => {
    expect(rules('a.css', '.a { font-family: "Inter", sans-serif; }')).toEqual(['font-family'])
    expect(rules('a.css', '.a { font-family: monospace; }')).toEqual(['font-family'])
    expect(rules('a.css', '.a { font: 13px "Inter"; }')).toEqual(['px-font', 'font-family'])
    expect(rules('a.css', '.a { font-family: var(--sh-mono); }')).toEqual([])
    expect(rules('a.css', '.a { font-family: inherit; font: inherit; }')).toEqual([])
    expect(rules('a.css', '.a { font: var(--sh-t-body)/var(--sh-lh) var(--sh-font); }')).toEqual([])
    expect(rules('a.css', '@font-face { font-family: "IBM Plex Sans"; src: url(a.woff2); }')).toEqual([])
    expect(rules('A.tsx', `<b style={{ fontFamily: 'Inter' }} />`)).toEqual(['font-family'])
  })

  it('reports the line it is on', () => {
    expect(lintText('a.css', '/* one\n two */\n.a {\n color: #fff;\n}')[0]?.line).toBe(4)
  })

  it('lets a file keep what the allowlist grants and fails the next one', () => {
    const findings = lintText('a.css', '.a { color: #fff; }\n.b { color: #000; }\n.c { font-size: 12px; }')
    expect(overAllowance('a.css', findings, { 'a.css': { hex: 2, 'px-font': 1 } })).toEqual([])
    expect(overAllowance('a.css', findings, { 'a.css': { hex: 1, 'px-font': 1 } }).map((f) => f.rule)).toEqual(['hex'])
    expect(overAllowance('a.css', findings, {}).length).toBe(3)
    expect(count(findings)).toEqual({ hex: 2, 'px-font': 1 })
  })
})

describe('style lint: the tree', () => {
  const found = scan()

  if (process.env.UPDATE_STYLE_ALLOWLIST) {
    it('writes the allowlist', () => {
      const out: Allowlist = {}
      for (const [file, findings] of Object.entries(found)) out[file] = count(findings)
      writeFileSync(join(HERE, 'styleAllowlist.json'), JSON.stringify(out, null, 2) + '\n')
    })
  }

  it('has no hex colour, px font size or font family outside the tokens beyond the allowlist', () => {
    const over = Object.entries(found).flatMap(([file, findings]) =>
      overAllowance(file, findings, allowlist).map((f) => `${file}:${f.line} ${f.rule}: ${f.text}`),
    )
    expect(over, 'Use a --sh-* token or a .sh-* class. New offenders fail; the allowlist only shrinks.').toEqual([])
  })

  it('lists no file the tree no longer has, so the allowlist shrinks with the code', () => {
    const gone = Object.keys(allowlist).filter((file) => !statSync(join(WEB, file), { throwIfNoEntry: false }))
    expect(gone).toEqual([])
  })

  it('exempts only the two files the design owns', () => {
    expect(EXEMPT).toEqual(['src/styles/tokens.css', 'src/styles/components.css'])
  })
})
