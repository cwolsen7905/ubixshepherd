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
  void text
  throw new Error('not implemented')
}
