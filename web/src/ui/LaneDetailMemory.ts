import type { LaneDetailTab } from './LaneDetail'

// What the person had open in a lane: the tab, and how far each tab was scrolled. Both forms of
// LaneDetail (the side panel and the page) read it, so "Open as page" lands where they were.
const KEY = 'shepherd.lane-detail'

interface Memory {
  tabs: Record<string, LaneDetailTab>
  scroll: Record<string, number>
}

function read(): Memory {
  try {
    const m = JSON.parse(window.sessionStorage.getItem(KEY) ?? '') as Partial<Memory>
    return { tabs: m.tabs ?? {}, scroll: m.scroll ?? {} }
  } catch {
    return { tabs: {}, scroll: {} }
  }
}

function write(m: Memory) {
  try {
    window.sessionStorage.setItem(KEY, JSON.stringify(m))
  } catch {
    // Storage may be full or off: the view just forgets.
  }
}

export function rememberedTab(laneId: number): LaneDetailTab | undefined {
  return read().tabs[laneId]
}

export function rememberTab(laneId: number, tab: LaneDetailTab) {
  const m = read()
  m.tabs[laneId] = tab
  write(m)
}

export function rememberedScroll(laneId: number, tab: LaneDetailTab): number {
  return read().scroll[`${laneId}/${tab}`] ?? 0
}

export function rememberScroll(laneId: number, tab: LaneDetailTab, top: number) {
  const m = read()
  m.scroll[`${laneId}/${tab}`] = Math.round(top)
  write(m)
}

/** The nearest ancestor that scrolls, else the page. */
export function scroller(el: HTMLElement | null): HTMLElement {
  for (let p = el?.parentElement; p; p = p.parentElement) {
    const y = window.getComputedStyle(p).overflowY
    if (y === 'auto' || y === 'scroll') return p
  }
  return (document.scrollingElement as HTMLElement | null) ?? document.documentElement
}
