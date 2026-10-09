// A run's log as blocks for the viewer. Shepherd writes each run's log (internal/dispatch)
// as plain lines:
//   "# shepherd run 40: claude in lane ..."   header lines, at the top, starting "# "
//   "→ Bash {\"command\":\"ls\"}"              one line per tool call: "→ ", the tool's
//                                             name, a space, its input (often JSON, clipped)
//   anything else                             the agent's own text, or a CLI's plain output

export type Block =
  | { kind: 'header'; lines: string[] }
  | { kind: 'tools'; calls: ToolCall[] }
  | { kind: 'text'; lines: string[] }

export interface ToolCall {
  /** The tool's name: "Bash", "mcp__shepherd__report". */
  tool: string
  /** Everything after the name and its space; "" when the line has no input. */
  input: string
  /** A one-line summary of the input for the collapsed view: for JSON input with a "command", "file_path", "path", "pattern", "url" or "description" field, the first of those found, in that order; otherwise the input itself. At most 120 characters, cut with "…". */
  summary: string
}

/**
 * Splits a log into blocks, in order. Consecutive lines of the same kind join one block:
 * header lines ("# " at the start of a line, only before any other non-empty line) into a
 * header block; consecutive tool call lines ("→ " at the start) into one tools block, so
 * the viewer can collapse a run of tool calls; everything else into text blocks. Empty
 * lines belong to the text block they are in, never start a block on their own, and are
 * dropped at the start and end of a block; empty lines between two tool lines are dropped
 * and do not split the tools block. A trailing "\n" adds no empty line. A "# "
 * line after the header is text. "" gives [].
 */
export function parseLog(text: string): Block[] {
  const lines = text.split('\n')
  if (lines.at(-1) === '') lines.pop()
  const blocks: Block[] = []
  let i = 0

  const header: string[] = []
  for (; i < lines.length; i++) {
    const line = lines[i] ?? ''
    if (line.startsWith('# ')) header.push(line)
    else if (line !== '' || header.length > 0) break
  }
  if (header.length > 0) blocks.push({ kind: 'header', lines: header })

  while (i < lines.length) {
    const calls: ToolCall[] = []
    for (; i < lines.length; i++) {
      const line = lines[i] ?? ''
      if (line.startsWith(TOOL)) calls.push(toolCall(line))
      else if (line !== '' || calls.length === 0) break
    }
    if (calls.length > 0) blocks.push({ kind: 'tools', calls })

    const text: string[] = []
    for (; i < lines.length && !(lines[i] ?? '').startsWith(TOOL); i++) text.push(lines[i] ?? '')
    while (text[0] === '') text.shift()
    while (text.at(-1) === '') text.pop()
    if (text.length > 0) blocks.push({ kind: 'text', lines: text })
  }
  return blocks
}

const TOOL = '→ '

function toolCall(line: string): ToolCall {
  const rest = line.slice(TOOL.length)
  const sp = rest.indexOf(' ')
  const tool = sp < 0 ? rest : rest.slice(0, sp)
  const input = sp < 0 ? '' : rest.slice(sp + 1)
  return { tool, input, summary: summarize(input) }
}

const SUMMARY_FIELDS = ['command', 'file_path', 'path', 'pattern', 'url', 'description']

function summarize(input: string): string {
  return clip(oneLine(field(input) ?? input.trim()), 120)
}

/** The first summary field of the input, from JSON, or from clipped JSON by pattern. */
function field(input: string): string | undefined {
  try {
    const v: unknown = JSON.parse(input)
    if (v && typeof v === 'object') {
      const o = v as Record<string, unknown>
      for (const f of SUMMARY_FIELDS) if (typeof o[f] === 'string') return o[f]
    }
    return undefined
  } catch {
    // Clipped mid-JSON: find the field as "name":"value" in the raw text.
    for (const f of SUMMARY_FIELDS) {
      const m = new RegExp(`"${f}"\\s*:\\s*"((?:[^"\\\\]|\\\\.)*)`).exec(input)
      if (m?.[1] !== undefined) return m[1]
    }
    return undefined
  }
}

function oneLine(s: string): string {
  return s.replace(/\\n|\n/g, ' ')
}

function clip(s: string, n: number): string {
  return s.length > n ? s.slice(0, n - 1) + '…' : s
}
