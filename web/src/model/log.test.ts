import { describe, it, expect } from 'vitest'

import { parseLog } from './log'

describe('parseLog', () => {
  it('returns empty array for empty input', () => {
    expect(parseLog('')).toEqual([]);
  });

  it('parses header followed by text', () => {
    const input = `# shepherd run 40: claude in lane ...
Hello world`;
    const result = parseLog(input);
    expect(result).toEqual([
      { kind: 'header', lines: ['# shepherd run 40: claude in lane ...'] },
      { kind: 'text', lines: ['Hello world'] }
    ]);
  });

  it('parses consecutive tool calls as one tools block', () => {
    const input = `→ Bash {"command":"ls"}
→ Bash {"command":"pwd"}`;
    const result = parseLog(input);
    expect(result).toEqual([
      { 
        kind: 'tools', 
        calls: [
          { tool: 'Bash', input: '{"command":"ls"}', summary: 'ls' },
          { tool: 'Bash', input: '{"command":"pwd"}', summary: 'pwd' }
        ] 
      }
    ]);
  });

  it('empty line between tool calls does not split them', () => {
    const input = `→ Bash {"command":"ls"}

→ Bash {"command":"pwd"}`;
    const result = parseLog(input);
    expect(result).toEqual([
      { 
        kind: 'tools', 
        calls: [
          { tool: 'Bash', input: '{"command":"ls"}', summary: 'ls' },
          { tool: 'Bash', input: '{"command":"pwd"}', summary: 'pwd' }
        ] 
      }
    ]);
  });

  it('handles text, tools, text alternation', () => {
    const input = `# header
Hello
→ Bash {"command":"ls"}
World`;
    const result = parseLog(input);
    expect(result).toEqual([
      { kind: 'header', lines: ['# header'] },
      { kind: 'text', lines: ['Hello'] },
      { 
        kind: 'tools', 
        calls: [
          { tool: 'Bash', input: '{"command":"ls"}', summary: 'ls' }
        ] 
      },
      { kind: 'text', lines: ['World'] }
    ]);
  });

  it('parses "# " line after header as text', () => {
    const input = `# shepherd run 40: claude in lane ...
# This is a comment
Hello`;
    const result = parseLog(input);
    expect(result).toEqual([
      { kind: 'header', lines: ['# shepherd run 40: claude in lane ...'] },
      { kind: 'text', lines: ['# This is a comment', 'Hello'] }
    ]);
  });

  it('drops trailing newline and leading/trailing empty lines in text block', () => {
    const input = `# header

Hello


World

`;
    const result = parseLog(input);
    expect(result).toEqual([
      { kind: 'header', lines: ['# header'] },
      { kind: 'text', lines: ['Hello', '', 'World'] }
    ]);
  });

  it('handles tool line with no input', () => {
    const input = `→ Bash`;
    const result = parseLog(input);
    expect(result).toEqual([
      { 
        kind: 'tools', 
        calls: [
          { tool: 'Bash', input: '', summary: '' }
        ] 
      }
    ]);
  });

  it('extracts summary from valid JSON with command field', () => {
    const input = `→ Bash {"command":"ls -la /tmp"}`;
    const result = parseLog(input);
    expect(result[0].calls[0].summary).toBe('ls -la /tmp');
  });

  it('extracts summary from valid JSON with file_path field when no command', () => {
    const input = `→ File {"file_path":"/home/user/file.txt"}`;
    const result = parseLog(input);
    expect(result[0].calls[0].summary).toBe('/home/user/file.txt');
  });

  it('extracts summary from clipped JSON like {"command":"ls -la /tmp","descr...', () => {
    const input = `→ Bash {"command":"ls -la /tmp","description":"List files"}...`;
    const result = parseLog(input);
    expect(result[0].calls[0].summary).toBe('ls -la /tmp');
  });

  it('summarizes to at most 120 characters with "…"', () => {
    const longInput = '→ Bash {"command":"a'.padEnd(130, 'b') + '"}';
    const result = parseLog(longInput);
    expect(result[0].calls[0].summary).toBe('a' + 'b'.repeat(117) + '…');
  });
});