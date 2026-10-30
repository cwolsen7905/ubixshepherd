import { placeFlyout } from './Flyout'

const rect = (left: number, top: number, w = 40, h = 20) => ({ left, top, right: left + w, bottom: top + h, width: w, height: h }) as DOMRect

describe('placeFlyout', () => {
  const view = { width: 1000, height: 700 }
  const size = { width: 350, height: 200 }

  it('goes to the right of the anchor, level with its bottom', () => {
    expect(placeFlyout(rect(100, 300), size, view)).toEqual({ left: 148, top: 120, side: 'right' })
  })

  it('flips to the left when the right edge would cut it', () => {
    expect(placeFlyout(rect(800, 300), size, view)).toEqual({ left: 442, top: 120, side: 'left' })
  })

  it('stays inside the window at the top and bottom', () => {
    expect(placeFlyout(rect(100, 10), size, view).top).toBe(8)
    expect(placeFlyout(rect(100, 690), size, view).top).toBe(492)
  })

  it('stays inside the window when neither side has room', () => {
    const p = placeFlyout(rect(100, 300), { width: 380, height: 100 }, { width: 400, height: 700 })
    expect(p.left).toBeGreaterThanOrEqual(8)
    expect(p.left + 380).toBeLessThanOrEqual(392)
  })
})
