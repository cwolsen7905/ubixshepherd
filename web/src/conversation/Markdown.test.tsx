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

  it('keeps continuation lines inside the item and returns to the outer level', () => {
    expect(lists('1. one\n   more of one\n   - nested\n2. two')).toEqual(['ol(one\nmore of one ul(nested) two)'])
  })
})
