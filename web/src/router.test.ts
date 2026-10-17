import { NAV } from './shell/nav'
import { canonical, formatOpen, href, parse, parseOpen, withOpen } from './router'

describe('router', () => {
  it('parses every page and round-trips its href', () => {
    expect(parse('')).toEqual({ page: 'chat' })
    expect(parse(href.board())).toEqual({ page: 'board' })
    expect(parse(href.inbox())).toEqual({ page: 'inbox' })
    expect(parse(href.inbox('mr'))).toEqual({ page: 'inbox', kind: 'mr' })
    // the old Decisions page redirects to the Inbox filtered to decisions
    expect(parse('#/decisions')).toEqual({ page: 'inbox', kind: 'decision' })
    expect(parse('#/decisions?d=9')).toEqual({ page: 'inbox', kind: 'decision', open: { kind: 'decision', id: 9 } })
    expect(canonical('#/decisions?d=9')).toBe('#/inbox?kind=decision&open=decision%3A9')
    expect(canonical('#/board')).toBe('#/board')
    expect(parse(href.projects())).toEqual({ page: 'projects' })
    expect(parse(href.project('shop'))).toEqual({ page: 'project', name: 'shop' })
    expect(parse(href.project('a b/c'))).toEqual({ page: 'project', name: 'a b/c' })
    expect(parse(href.unassigned())).toEqual({ page: 'unassigned' })
    expect(parse(href.chat())).toEqual({ page: 'chat' })
    expect(href.status()).toBe('#/status')
    expect(parse(href.status())).toEqual({ page: 'status' })
    expect(parse('#/status?open=run:3')).toEqual({ page: 'status', open: { kind: 'run', id: 3 } })
    expect(parse(href.next())).toEqual({ page: 'next' })
    expect(href.next()).toBe('#/next')
    expect(href.next(7)).toBe('#/next?item=7')
    expect(parse(href.next(7))).toEqual({ page: 'next', focus: 7 })
    expect(parse('#/next?item=x')).toEqual({ page: 'next' })
    expect(parse('#/chat?x=1')).toEqual({ page: 'chat' })
    expect(parse(href.lane(4))).toEqual({ page: 'lane', id: 4 })
    expect(parse(href.log(12))).toEqual({ page: 'log', run: 12 })
    expect(parse(href.board('week'))).toEqual({ page: 'board', range: 'week' })
    expect(parse('#/?range=bogus')).toEqual({ page: 'chat' })
    expect(parse(href.runs())).toEqual({ page: 'runs' })
    const f = { range: 'week', agent: 'claude', lane: 7 } as const
    expect(parse(href.runs(f))).toEqual({ page: 'runs', ...f })
    expect(parse(href.usage())).toEqual({ page: 'usage' })
    expect(parse(href.usage({ range: 'month' }))).toEqual({ page: 'usage', range: 'month' })
    const u = { range: 'custom', from: '2026-10-01', to: '2026-10-07' } as const
    expect(parse(href.usage(u))).toEqual({ page: 'usage', ...u })
    expect(parse('#/usage?range=custom&from=nope&to=2026-10-07')).toEqual({ page: 'usage', range: 'custom', to: '2026-10-07' })
    expect(parse('#/usage?range=week&from=2026-10-01')).toEqual({ page: 'usage', range: 'week' })
    expect(parse(href.timeline())).toEqual({ page: 'timeline' })
    expect(parse(href.timeline({ range: '24h' }))).toEqual({ page: 'timeline', range: '24h' })
    expect(parse(href.timeline({ range: 'week', agent: 'claude' }))).toEqual({ page: 'timeline', range: 'week', agent: 'claude' })
    expect(parse('#/timeline?range=month&agent=')).toEqual({ page: 'timeline' })
    expect(href.timeline({ range: 'today' })).toBe('#/timeline?range=today')
    expect(parse('#/nope')).toEqual({ page: 'missing', path: '/nope' })
    // home is the conversation, board moved to #/board
    expect(href.chat()).toBe('#/')
    expect(parse('#/')).toEqual({ page: 'chat' })
    expect(parse('#/chat')).toEqual({ page: 'chat' })
    expect(href.board()).toBe('#/board')
    expect(parse('#/board')).toEqual({ page: 'board' })
    expect(parse('#/board?range=week')).toEqual({ page: 'board', range: 'week' })
  })
})

describe('cameFrom', () => {
  it('remembers the page left and its scroll, and ignores unknown pages', async () => {
    const { cameFrom } = await import('./router')
    const wait = () => new Promise((r) => setTimeout(r, 0))
    window.location.hash = '#/?range=week'
    await wait()
    window.location.hash = '#/runs/3/log'
    await wait()
    expect(cameFrom()).toEqual({ hash: '#/?range=week', scroll: 0 })
    window.location.hash = '#/nowhere/at/all'
    await wait()
    window.location.hash = '#/lanes/1'
    await wait()
    expect(cameFrom()).toBeUndefined()
  })
})

describe('open=', () => {
  it('parses kind, id and tab on any route', () => {
    expect(parse('#/?open=decision:5')).toEqual({ page: 'chat', open: { kind: 'decision', id: 5 } })
    expect(parse('#/board?open=lane:4/changes&range=week')).toEqual({
      page: 'board',
      range: 'week',
      open: { kind: 'lane', id: 4, tab: 'changes' },
    })
    expect(parse('#/runs/3/log?open=run:17/log')).toEqual({ page: 'log', run: 3, open: { kind: 'run', id: 17, tab: 'log' } })
    expect(parse('#/board?open=mr:4/changes')).toEqual({ page: 'board', open: { kind: 'mr', id: 4, tab: 'changes' } })
    expect(parse('#/board?open=lane:4/artifacts/full')).toEqual({ page: 'board', open: { kind: 'lane', id: 4, tab: 'artifacts', full: true } })
    expect(parse('#/board?open=run:4/full')).toEqual({ page: 'board', open: { kind: 'run', id: 4, full: true } })
    expect(parse('#/nope?open=artifact:12')).toEqual({ page: 'missing', path: '/nope', open: { kind: 'artifact', id: 12 } })
  })

  it('ignores a value it cannot read', () => {
    for (const bad of ['decision', 'decision:', 'decision:0', 'decision:x', 'bogus:1', 'lane:1/', 'lane:-1', 'decision:99999999999999999999']) {
      expect(parse(`#/board?open=${bad}`)).toEqual({ page: 'board' })
    }
    expect(parseOpen(null)).toBeUndefined()
  })

  it('round-trips and edits the hash', () => {
    const o = { kind: 'run', id: 17, tab: 'log' }
    expect(parseOpen(formatOpen(o))).toEqual(o)
    expect(withOpen('#/', { kind: 'decision', id: 5 })).toBe('#/?open=decision:5'.replace(':', '%3A'))
    expect(withOpen('#/board?range=week', o)).toBe('#/board?range=week&open=run%3A17%2Flog')
    expect(withOpen('#/board?range=week&open=run%3A17', undefined)).toBe('#/board?range=week')
    expect(withOpen('#/?open=decision:5', undefined)).toBe('#/')
    expect(parse(withOpen('#/board', o))).toEqual({ page: 'board', open: o })
    const full = { kind: 'lane', id: 4, tab: 'changes', full: true }
    expect(parse(withOpen('#/board', full))).toEqual({ page: 'board', open: full })
    expect(parse(withOpen('#/board', { kind: 'mr', id: 4, full: true }))).toEqual({ page: 'board', open: { kind: 'mr', id: 4, full: true } })
  })
})

describe('the view table', () => {
  const VIEWS = ['Conversation', 'Inbox', 'Board', 'Next up', 'Artifacts', 'Files', 'Runs', 'Timeline', 'History', 'Projects', 'Rules', 'Usage', 'Settings', 'Status']

  it('lists every view in the nav, in the design order, each reaching its own route', () => {
    const items = NAV.flatMap((g) => g.items)
    expect(items.map((i) => i.label)).toEqual(VIEWS)
    for (const i of items) {
      const r = parse(i.to())
      expect(r.page).not.toBe('missing')
      expect(items.filter((o) => o.current(r)).map((o) => o.label)).toEqual([i.label])
    }
  })

  it('parses and round-trips the new routes', () => {
    expect(parse(href.history())).toEqual({ page: 'history' })
    expect(parse(href.history({ kind: 'run', project: 'a b', q: 'login' }))).toEqual({ page: 'history', kind: 'run', project: 'a b', q: 'login' })
    expect(parse(href.files())).toEqual({ page: 'files' })
    expect(parse(href.files({ repo: 2, lane: 5, ref: 'dev' }))).toEqual({ page: 'files', repo: 2, lane: 5, ref: 'dev' })
    expect(parse('#/files?lane=x')).toEqual({ page: 'files' })
    expect(parse(href.rules())).toEqual({ page: 'rules' })
  })
})
