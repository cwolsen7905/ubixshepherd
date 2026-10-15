import { laneTab } from './lane'

describe('laneTab', () => {
  it('reads the tab from the address and defaults to the overview', () => {
    expect(laneTab('changes')).toBe('changes')
    expect(laneTab('artifacts')).toBe('artifacts')
    expect(laneTab(undefined)).toBe('overview')
    expect(laneTab('bogus')).toBe('overview')
  })
})
