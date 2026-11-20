import { render } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { Markdown } from './Markdown'

function html(text: string): HTMLElement {
  return render(<Markdown text={text} />).container
}

/** The list's shape as text: `ol(a ul(b c))`. */
function shape(el: Element): string {
  const tag = el.tagName.toLowerCase()
  const parts = Array.from(el.children).map((li) => {
    const label = Array.from(li.childNodes)
      .filter((n) => n.nodeType === Node.TEXT_NODE)
      .map((n) => n.textContent)
      .join('')
      .trim()
    const subs = Array.from(li.children).map(shape)
    return [label, ...subs].join(' ')
  })
  return `${tag}(${parts.join(' ')})`
}

function lists(text: string): string[] {
  return Array.from(html(text).firstElementChild?.children ?? [])
    .filter((e) => e.tagName === 'OL' || e.tagName === 'UL')
    .map(shape)
}

describe('Markdown lists', () => {
  it('nests bullets under a numbered item', () => {
    expect(lists('1. one\n   - a\n   - b\n2. two')).toEqual(['ol(one ul(a b) two)'])
  })

  it('nests numbers under a bullet', () => {
    expect(lists('- one\n  1. a\n  2. b\n- two')).toEqual(['ul(one ol(a b) two)'])
  })

  it('nests three levels, with a tab as an indent', () => {
    expect(lists('- a\n  - b\n\t\t- c\n- d')).toEqual(['ul(a ul(b ul(c)) d)'])
    expect(lists('1. a\n  - b\n    1. c')).toEqual(['ol(a ul(b ol(c)))'])
  })

  it('starts a bulleted list after a numbered one', () => {
    expect(lists('1. one\n2. two\n- x\n- y')).toEqual(['ol(one two)', 'ul(x y)'])
  })

  it('starts a numbered list after bullets', () => {
    expect(lists('- x\n- y\n1. one\n2. two')).toEqual(['ul(x y)', 'ol(one two)'])
  })

  it('nests under a replacement list started at a shallower indent', () => {
    expect(lists('  1. a\n- b\n   - c')).toEqual(['ol(a)', 'ul(b ul(c))'])
  })

  it('closes a list at an unindented line, so the next marker starts a new list', () => {
    const root = html('- one\nnote\n- two').firstElementChild
    expect(Array.from(root?.children ?? []).map((e) => e.tagName)).toEqual(['UL', 'P', 'UL'])
  })

  it('keeps sub-items at an inconsistent indent in one list', () => {
    expect(lists('- a\n    - b\n  - c')).toEqual(['ul(a ul(b c))'])
  })

  it('keeps continuation lines inside the item and returns to the outer level', () => {
    expect(lists('1. one\n   more of one\n   - nested\n2. two')).toEqual(['ol(one\nmore of one ul(nested) two)'])
  })
})

describe('Markdown prefixed ids', () => {
  it('links an id in prose and in bold, to the side column', () => {
    window.location.hash = '#/chat'
    const c = html('Answer **D-370** and look at Q-196.')
    const links = Array.from(c.querySelectorAll('a[data-component="IdLink"]'))
    expect(links.map((a) => a.textContent)).toEqual(['D-370', 'Q-196'])
    expect(links[0]?.getAttribute('href')).toBe('#/chat?open=decision%3A370')
  })

  it('keeps the current page and its other parameters', () => {
    window.location.hash = '#/runs?range=week'
    const a = html('R-1674').querySelector('a')
    expect(a?.getAttribute('href')).toBe('#/runs?range=week&open=run%3A1674')
  })

  it('does not link in code spans, fenced code or link labels', () => {
    const c = html('`Q-1` and [N-2](https://example.com)\n\n```\nD-3\n```')
    expect(c.querySelectorAll('a[data-component="IdLink"]')).toHaveLength(0)
    expect(c.textContent).toContain('Q-1')
  })

  it('links in lists, headings and tables', () => {
    const c = html('# L-5\n\n- T-6\n\n| a |\n|---|\n| N-7 |')
    expect(c.querySelectorAll('a[data-component="IdLink"]')).toHaveLength(3)
  })
})
