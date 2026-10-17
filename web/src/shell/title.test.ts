import { parse } from '../router'
import { pageTitle } from './title'

const lanes = [{ id: 7, name: 'feat/export-csv' }]

describe('pageTitle', () => {
  it('names the lane on the lane route, and falls back to its id', () => {
    expect(pageTitle(parse('#/lanes/7'), lanes)).toBe('feat/export-csv')
    expect(pageTitle(parse('#/lanes/9'), lanes)).toBe('Lane 9')
  })
  it('follows the route where the nav item is broader', () => {
    expect(pageTitle(parse('#/runs/12/log'))).toBe('Run 12 log')
    expect(pageTitle(parse('#/projects/acme'))).toBe('acme')
    expect(pageTitle(parse('#/status'))).toBe('Status')
  })
  it('uses the nav label for the plain pages', () => {
    expect(pageTitle(parse('#/board'))).toBe('Board')
    expect(pageTitle(parse('#/chat'))).toBe('Conversation')
    expect(pageTitle(parse('#/next'))).toBe('Next up')
  })
})
