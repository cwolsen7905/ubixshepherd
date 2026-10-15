import { Fragment, type ReactNode } from 'react'
import { CopyButton } from '../components/CopyButton'

// What an agent wrote is text, never markup: elements are built here, so nothing of it
// reaches innerHTML, and a link survives only as an http or https anchor.

function safeHref(raw: string): string | null {
  try {
    const u = new URL(raw)
    return u.protocol === 'http:' || u.protocol === 'https:' ? u.href : null
  } catch {
    return null
  }
}

const INLINE = /(`[^`\n]+`|\*\*[^*\n]+\*\*|\[[^\]\n]+\]\([^)\s]+\))/

/** Inline markdown only (code, bold, links), for a line that sits inside a heading or a button. */
export function inline(text: string): ReactNode[] {
  return text.split(INLINE).map((part, i) => {
    if (part.length > 2 && part.startsWith('`') && part.endsWith('`')) return <code key={i}>{part.slice(1, -1)}</code>
    if (part.length > 4 && part.startsWith('**') && part.endsWith('**')) return <strong key={i}>{part.slice(2, -2)}</strong>
    const m = /^\[([^\]]+)\]\(([^)\s]+)\)$/.exec(part)
    if (m) {
      const url = safeHref(m[2] ?? '')
      if (url) {
        return (
          <a key={i} href={url} target="_blank" rel="noopener noreferrer">
            {m[1]}
          </a>
        )
      }
      return <Fragment key={i}>{`${m[1] ?? ''} (${m[2] ?? ''})`}</Fragment>
    }
    return <Fragment key={i}>{part}</Fragment>
  })
}

export type TaskItem = { checked: boolean; text: string }

type Block =
  | { t: 'p'; text: string }
  | { t: 'h'; text: string }
  | { t: 'code'; text: string }
  | { t: 'list'; ordered: boolean; items: string[] }
  | { t: 'task'; items: TaskItem[] }
  | { t: 'quote'; text: string }
  | { t: 'hr' }
  | { t: 'table'; header: string[]; rows: string[][] }

/** A small markdown subset: paragraphs, headings, fenced code, lists, tables, blockquotes, horizontal rules and task lists. */
export function blocks(src: string): Block[] {
  const out: Block[] = []
  const lines = src.replace(/\r\n?/g, '\n').split('\n')
  let para: string[] = []
  let listItems: string[] = []
  let listOrdered = false
  let taskItems: TaskItem[] = []
  let quoteLines: string[] = []
  const flush = () => {
    if (para.length) out.push({ t: 'p', text: para.join('\n') })
    para = []
  }
  const flushList = () => {
    if (listItems.length) {
      out.push({ t: 'list', ordered: listOrdered, items: [...listItems] })
      listItems = []
      listOrdered = false
    }
  }
  const flushTask = () => {
    if (taskItems.length) {
      out.push({ t: 'task', items: [...taskItems] })
      taskItems = []
    }
  }
  const flushQuote = () => {
    if (quoteLines.length) {
      out.push({ t: 'quote', text: quoteLines.join('\n') })
      quoteLines = []
    }
  }
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i] ?? ''
    // Fenced code
    if (/^\s*```/.test(line)) {
      flush()
      flushList()
      flushTask()
      flushQuote()
      const code: string[] = []
      for (i++; i < lines.length && !/^\s*```/.test(lines[i] ?? ''); i++) code.push(lines[i] ?? '')
      out.push({ t: 'code', text: code.join('\n') })
      continue
    }
    // Horizontal rule
    if (/^(-{3,}|_{3,}|\*{3,})$/.test(line.trim())) {
      flush()
      flushList()
      flushTask()
      flushQuote()
      out.push({ t: 'hr' })
      continue
    }
    // Heading
    const h = /^#{1,6}\s+(.*)$/.exec(line)
    if (h) {
      flush()
      flushList()
      flushTask()
      flushQuote()
      out.push({ t: 'h', text: h[1] ?? '' })
      continue
    }
    // Blockquote
    const bq = /^\>\s?(.*)$/.exec(line)
    if (bq) {
      flush()
      flushList()
      flushTask()
      quoteLines.push(bq[1] ?? '')
      continue
    }
    // Table separator row (|---|---|) must come before generic pipe check
    if (/^\|[\s:|-]+\|$/.test(line.trim())) {
      const last = out.at(-1)
      if (last?.t === 'table') {
        continue // skip separator
      }
      // Not in a table; fall through to treat as regular content
    }
    // Table row (pipe table)
    if (/^\|/.test(line)) {
      flush()
      flushList()
      flushTask()
      flushQuote()
      const cells = line.split('|').slice(1, -1).map((c) => c.trim())
      const last = out.at(-1)
      if (last?.t === 'table') {
        last.rows.push(cells)
      } else {
        out.push({ t: 'table', header: cells, rows: [] })
      }
      continue
    }
    // Task list item (- [ ] or - [x])
    const task = /^\s*[-*+]\s+\[([ xX])\]\s+(.*)$/.exec(line)
    if (task) {
      flush()
      flushList()
      flushQuote()
      taskItems.push({ checked: task[1] !== ' ', text: task[2] ?? '' })
      continue
    }
    // Regular list item
    const li = /^\s*(?:([-*])|\d+[.)])\s+(.*)$/.exec(line)
    if (li) {
      flush()
      flushTask()
      flushQuote()
      const ordered = !li[1]
      if (listItems.length === 0) {
        listOrdered = ordered
      }
      listItems.push(li[2] ?? '')
      continue
    }
    // Blank line flushes
    if (line.trim() === '') {
      flush()
      flushList()
      flushTask()
      flushQuote()
      continue
    }
    // Regular text
    para.push(line)
  }
  flush()
  flushList()
  flushTask()
  flushQuote()
  return out
}

/** Class names for each block: the lane pages' `cv-` set, or the chat page's `md` set. */
const CLASSES = {
  cv: { root: 'cv-md', h: 'cv-md-h', task: 'cv-task', quote: 'cv-quote', hr: 'cv-hr', table: 'cv-table' },
  md: { root: 'md', h: 'md-h', task: 'md-task', quote: 'md-quote', hr: 'md-hr', table: 'md-table' },
}

export function Markdown({ text, classes = 'cv' }: { text: string; classes?: keyof typeof CLASSES }) {
  const c = CLASSES[classes]
  return (
    <div className={c.root}>
      {blocks(text).map((b, i) => {
        switch (b.t) {
          case 'code':
            return (
              <div key={i} className="cv-code">
                <pre>
                  <code>{b.text}</code>
                </pre>
                <CopyButton text={b.text.replace(/\n+$/, '')} />
              </div>
            )
          case 'h':
            return (
              <p key={i} className={c.h}>
                {inline(b.text)}
              </p>
            )
          case 'list': {
            const items = b.items.map((it, j) => <li key={j}>{inline(it)}</li>)
            return b.ordered ? <ol key={i}>{items}</ol> : <ul key={i}>{items}</ul>
          }
          case 'task': {
            const items = (b.items as TaskItem[]).map((it, j) => (
              <li key={j}>
                <input type="checkbox" disabled checked={it.checked} /> {inline(it.text)}
              </li>
            ))
            return <ul key={i} className={c.task}>{items}</ul>
          }
          case 'quote':
            return (
              <blockquote key={i} className={c.quote}>
                <p>{inline(b.text)}</p>
              </blockquote>
            )
          case 'hr':
            return <hr key={i} className={c.hr} />
          case 'table': {
            const thead = b.header.map((h, j) => <th key={j}>{inline(h)}</th>)
            const tbody = b.rows.map((row, ri) => (
              <tr key={ri}>
                {row.map((cell, ci) => <td key={ci}>{inline(cell)}</td>)}
              </tr>
            ))
            return (
              <table key={i} className={c.table}>
                <thead>
                  <tr>{thead}</tr>
                </thead>
                <tbody>{tbody}</tbody>
              </table>
            )
          }
          default:
            return <p key={i}>{inline(b.text)}</p>
        }
      })}
    </div>
  )
}
