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
  if (!text) return [];
  
  const lines = text.split('\n');
  const blocks: Block[] = [];
  let i = 0;
  
  // Parse header lines (lines starting with "# ") at the top
  const headerLines: string[] = [];
  while (i < lines.length && lines[i].startsWith('# ')) {
    headerLines.push(lines[i]);
    i++;
  }
  
  // Add header block if there were any
  if (headerLines.length > 0) {
    blocks.push({ kind: 'header', lines: headerLines });
  }
  
  // Process the rest of the log, starting from where we left off (after headers)
  while (i < lines.length) {
    const line = lines[i];
    
    if (line.startsWith('→ ')) {
      // Tool call block - collect all consecutive tool calls
      const calls: ToolCall[] = [];
      
      while (i < lines.length && (lines[i].startsWith('→ ') || lines[i] === '')) {
        const toolLine = lines[i];
        
        if (toolLine.startsWith('→ ')) {
          i++;
          
          const spaceIndex = toolLine.indexOf(' ', 2);
          if (spaceIndex === -1) {
            // No input
            calls.push({
              tool: toolLine.substring(2),
              input: '',
              summary: ''
            });
          } else {
            const toolName = toolLine.substring(2, spaceIndex);
            const input = toolLine.substring(spaceIndex + 1);
            
            calls.push({
              tool: toolName,
              input: input,
              summary: extractSummary(input)
            });
          }
        } else {
          // Empty line - skip it but continue in this loop
          i++;
        }
      }
      
      blocks.push({ kind: 'tools', calls });
    } else {
      // Text block - collect all consecutive non-tool, non-empty lines
      const linesInBlock: string[] = [];
      
      while (i < lines.length && !lines[i].startsWith('→ ')) {
        linesInBlock.push(lines[i]);
        i++;
      }
      
      // Only create a text block if we have content  
      if (linesInBlock.length > 0) {
        // Remove leading and trailing empty lines in text block  
        let start = 0;
        let end = linesInBlock.length - 1;
        
        // Find first non-empty line
        while (start <= end && linesInBlock[start].trim() === '') {
          start++;
        }
        
        // Find last non-empty line
        while (end >= start && linesInBlock[end].trim() === '') {
          end--;
        }
        
        // Extract only the non-empty portion
        const trimmedLines = linesInBlock.slice(start, end + 1);
        
        if (trimmedLines.length > 0) {
          blocks.push({ kind: 'text', lines: trimmedLines });
        }
      }
    }
  }
  
  return blocks;
}

function extractSummary(input: string): string {
  const trimmedInput = input.trim();
  
  // If input is empty, return empty summary
  if (trimmedInput === '') {
    return '';
  }

  let parsedInput: any;
  
  // Try to parse as JSON first
  try {
    parsedInput = JSON.parse(trimmedInput);
  } catch {
    // If parsing fails, try with a regex approach to find the key-value pairs
    const fields = ['command', 'file_path', 'path', 'pattern', 'url', 'description'];
    for (const field of fields) {
      // Match pattern like "field": "value"
      const regex = new RegExp(`"${field}"\\s*:\\s*"([^"]*)"`, 'i');
      const match = trimmedInput.match(regex);
      
      if (match && match[1]) {
        const value = match[1].replace(/\n/g, ' ');
        // Truncate to exactly 120 chars + "…"
        return value.length <= 120 ? value : value.substring(0, 117) + '…';
      }
    }
    
    // If no fields found, fall back to raw input
    const value = trimmedInput.replace(/\n/g, ' ');
    return value.length <= 120 ? value : value.substring(0, 117) + '…';
  }
  
  if (parsedInput && typeof parsedInput === 'object') {
    const fields = ['command', 'file_path', 'path', 'pattern', 'url', 'description'];
    for (const field of fields) {
      if (typeof parsedInput[field] === 'string') {
        const value = parsedInput[field].replace(/\n/g, ' ');
        // Truncate to exactly 120 chars + "…"
        return value.length <= 120 ? value : value.substring(0, 117) + '…';
      }
    }
  }
  
  // If all else fails, use the raw input
  const value = trimmedInput.replace(/\n/g, ' ');
  // Properly truncate to exactly 117 chars with "…" for exact 120 character count
  return value.length <= 120 ? value : value.substring(0, 117) + '…';
}