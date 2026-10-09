import { boardItems, counts, countsText, laneClashes, noteOutcome } from './board'
import { decision, lane, request, run } from '../test/fixtures'

describe('laneClashes', () => {
  it('identifies lanes with same name but different repos', () => {
    const lanes = [
      lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
      lane({ id: 2, repo: 'other-api', name: 'feat/login' }),
      lane({ id: 3, repo: 'acme-api', name: 'feat/payment' }),
    ]
    const clashes = laneClashes(lanes)
    expect(clashes).toEqual(new Set(['feat/login']))
  })

  it('returns empty set when no lane names clash', () => {
    const lanes = [
      lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
      lane({ id: 2, repo: 'other-api', name: 'feat/payment' }),
    ]
    const clashes = laneClashes(lanes)
    expect(clashes).toEqual(new Set())
  })
})

describe('boardItems', () => {
  const seen = { until: '2026-10-08T09:00:00Z', keys: new Set<string>() }

  describe('needs group', () => {
    it('includes open decisions with detail starting "decision 5: "', () => {
      const decisions = [decision({ id: 5, question: 'Should we use authentication?' })]
      const items = boardItems({ lanes: [], runs: [], decisions, requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('needs')
      expect(items[0].detail).toMatch(/^decision 5: /)
    })

    it('includes requests with glyph "!"', () => {
      const requests = [request({ id: 3, message: 'What should we do about authentication?' })]
      const items = boardItems({ lanes: [], runs: [], decisions: [], requests, seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('needs')
      expect(items[0].glyph).toBe('!')
    })
  })

  describe('broken group', () => {
    it('includes failed or interrupted run with detail "run <id> failed"', () => {
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'failed' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('broken')
      expect(items[0].detail).toBe('run 10 failed')
    })

    it('includes pipeline failed lane with detail "pipeline failed"', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        mr_state: 'open',
        pipeline_status: 'failed'
      })]
      const runs = [run({ id: 10, lane_id: 1, state: 'succeeded' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('broken')
      expect(items[0].detail).toBe('pipeline failed')
    })

    it('includes running lane with pipeline failure with detail ending ", fixing"', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        mr_state: 'open',
        pipeline_status: 'failed'
      })]
      const runs = [run({ id: 10, lane_id: 1, state: 'running' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('broken')
      expect(items[0].detail).toBe('pipeline failed, fixing')
    })

    it('includes interrupted run with detail "run <id> interrupted"', () => {
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'interrupted' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('broken')
      expect(items[0].detail).toBe('run 10 interrupted')
    })
  })

  describe('working group', () => {
    it('includes lane with running latest run', () => {
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'running' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('working')
      expect(items[0].detail).toBe('run 10')
    })
  })

  describe('review group', () => {
    it('includes lane with open MR, pipeline status passed, and finished run seen already', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        mr_state: 'open',
        pipeline_status: 'passed'
      })]
      const runs = [run({ id: 10, lane_id: 1, state: 'succeeded' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('review')
      expect(items[0].detail).toBe('waiting for a merge')
    })

    it('includes lane with open MR and passed pipeline even without completed run', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        mr_state: 'open',
        pipeline_status: 'passed'
      })]
      const items = boardItems({ lanes, runs: [], decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('review')
    })
  })

  describe('done group', () => {
    it('includes lane with run ended after seen.until and key not in seen.keys', () => {
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'succeeded', ended: '2026-10-08T10:04:00Z' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('done')
      expect(items[0].detail).toBe('run 10 succeeded')
    })

    it('includes closed lane with merged MR and closed after seen.until', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        state: 'closed',
        mr_state: 'merged',
        closed: '2026-10-08T10:04:00Z'
      })]
      const items = boardItems({ lanes, runs: [], decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('done')
      expect(items[0].detail).toBe('merged')
    })

    it('does not include done item when key is in seen.keys', () => {
      const seenWithKeys = { until: '2026-10-08T09:00:00Z', keys: new Set(['run:10']) }
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'succeeded', ended: '2026-10-08T10:04:00Z' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen: seenWithKeys })
      expect(items).toHaveLength(0)
    })

    it('does not include done item when run ended before seen.until', () => {
      const seenBefore = { until: '2026-10-08T11:00:00Z', keys: new Set<string>() }
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const runs = [run({ id: 10, lane_id: 1, state: 'succeeded', ended: '2026-10-08T10:04:00Z' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen: seenBefore })
      expect(items).toHaveLength(0)
    })

    it('includes closed lane with merged MR even if closed before seen.until (but not if already seen)', () => {
      const seenWithKeys = { until: '2026-10-08T11:00:00Z', keys: new Set<string>() }
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        state: 'closed',
        mr_state: 'merged',
        closed: '2026-10-08T10:04:00Z'
      })]
      const items = boardItems({ lanes, runs: [], decisions: [], requests: [], seen: seenWithKeys })
      expect(items).toHaveLength(1) // should include the done item even though closed before seen.until
    })
  })

  describe('ordering', () => {
    it('orders groups correctly: needs, broken, review, working, done', () => {
      const lanes = [
        lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
        lane({ id: 2, repo: 'acme-api', name: 'feat/payment' }),
        lane({ id: 3, repo: 'acme-api', name: 'feat/auth' }),
        lane({ id: 4, repo: 'acme-api', name: 'feat/merge' }),
        lane({ id: 5, repo: 'acme-api', name: 'feat/done' }),
      ]
      const runs = [
        run({ id: 10, lane_id: 1, state: 'failed' }), // broken
        run({ id: 20, lane_id: 2, state: 'succeeded', ended: '2026-10-08T10:04:00Z' }), // done
        run({ id: 30, lane_id: 3, state: 'running' }), // working
        run({ id: 40, lane_id: 4, state: 'succeeded' }), // review (has open mr with passed pipeline)
      ]
      const decisions = [decision({ id: 5 })] // needs
      const items = boardItems({ lanes, runs, decisions, requests: [], seen })
      
      expect(items).toHaveLength(5)
      expect(items[0].group).toBe('needs')
      expect(items[1].group).toBe('broken')
      expect(items[2].group).toBe('review')
      expect(items[3].group).toBe('working')
      expect(items[4].group).toBe('done')
    })

    it('orders within groups from oldest to newest (except done which is newest first)', () => {
      const lanes = [
        lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
        lane({ id: 2, repo: 'acme-api', name: 'feat/payment' }),
      ]
      const runs = [
        run({ id: 10, lane_id: 1, state: 'failed', started: '2026-10-08T09:30:00Z' }), // broken
        run({ id: 20, lane_id: 2, state: 'running', started: '2026-10-08T10:00:00Z' }), // working
      ]
      
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      
      // within broken group (oldest first): item 1 started at 09:30
      expect(items[0].group).toBe('broken')
      expect(items[0].at).toBe('2026-10-08T09:30:00Z')
      
      // within working group (oldest first): item 2 started at 10:00
      expect(items[1].group).toBe('working')
      expect(items[1].at).toBe('2026-10-08T10:00:00Z')
      
      // done group (newest first): items from runs with latest ended times  
      const lanes2 = [
        lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
        lane({ id: 2, repo: 'acme-api', name: 'feat/payment' }),
      ]
      const runs2 = [
        run({ id: 10, lane_id: 1, state: 'succeeded', ended: '2026-10-08T10:03:00Z' }), // done  
        run({ id: 20, lane_id: 2, state: 'succeeded', ended: '2026-10-08T10:04:00Z' }), // done
      ]
      const items2 = boardItems({ lanes: lanes2, runs: runs2, decisions: [], requests: [], seen })
      
      // newest first in done group: item from run 20 (ended at 10:04)
      expect(items2[0].group).toBe('done')
      expect(items2[0].at).toBe('2026-10-08T10:04:00Z')
    })
  })

  describe('repo naming', () => {
    it('shows repo:lane name when two repos use the same lane name', () => {
      const lanes = [
        lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
        lane({ id: 2, repo: 'other-api', name: 'feat/login' }),
      ]
      const runs = [run({ id: 10, lane_id: 1, state: 'failed' }), run({ id: 20, lane_id: 2, state: 'running' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      
      expect(items).toHaveLength(2)
      expect(items[0].name).toBe('acme-api:feat/login') // First item
      expect(items[1].name).toBe('other-api:feat/login') // Second item
    })

    it('uses lane name only when repo names are different', () => {
      const lanes = [
        lane({ id: 1, repo: 'acme-api', name: 'feat/login' }),
        lane({ id: 2, repo: 'other-api', name: 'feat/payment' }),
      ]
      const runs = [run({ id: 10, lane_id: 1, state: 'failed' }), run({ id: 20, lane_id: 2, state: 'running' })]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      
      expect(items).toHaveLength(2)
      expect(items[0].name).toBe('feat/login') // First item
      expect(items[1].name).toBe('feat/payment') // Second item
    })
  })

  describe('edge cases', () => {
    it('handles lanes with no matching runs properly', () => {
      const lanes = [lane({ id: 1, repo: 'acme-api', name: 'feat/login' })]
      const items = boardItems({ lanes, runs: [], decisions: [], requests: [], seen })
      expect(items).toHaveLength(0)
    })

    it('includes lane with open MR and passed pipeline even when no run exists', () => {
      const lanes = [lane({ 
        id: 1, 
        repo: 'acme-api', 
        name: 'feat/login',
        mr_state: 'open',
        pipeline_status: 'passed'
      })]
      const items = boardItems({ lanes, runs: [], decisions: [], requests: [], seen })
      expect(items).toHaveLength(1)
      expect(items[0].group).toBe('review')
    })

    it('handles multiple lanes with same name in different repos correctly', () => {
      const lanes = [
        lane({ id: 1, repo: 'repo1', name: 'feature' }),
        lane({ id: 2, repo: 'repo2', name: 'feature' }),
        lane({ id: 3, repo: 'repo1', name: 'feature2' }),
      ]
      const runs = [
        run({ id: 10, lane_id: 1, state: 'failed' }),
        run({ id: 20, lane_id: 2, state: 'succeeded' }),
        run({ id: 30, lane_id: 3, state: 'running' }),
      ]
      const items = boardItems({ lanes, runs, decisions: [], requests: [], seen })
      
      expect(items).toHaveLength(3)
      expect(items[0].name).toBe('repo1:feature')
      expect(items[1].name).toBe('repo2:feature') 
      expect(items[2].name).toBe('feature2')
    })
  })
})

describe('counts', () => {
  it('returns counts for each group', () => {
    const items = [
      { group: 'needs' } as any,
      { group: 'broken' } as any,
      { group: 'broken' } as any,
      { group: 'review' } as any,
      { group: 'working' } as any,
      { group: 'done' } as any,
    ]
    const countsResult = counts(items)
    expect(countsResult).toEqual({
      needs: 1,
      broken: 2,
      review: 1,
      working: 1,
      done: 1
    })
  })
})

describe('countsText', () => {
  it('formats counts into text like "1 needs you · 2 broken"', () => {
    const countsResult = {
      needs: 1,
      broken: 2,
      review: 0,
      working: 3,
      done: 0
    }
    const text = countsText(countsResult)
    expect(text).toBe('1 needs you · 2 broken · 3 working')
  })

  it('omits groups with zero count', () => {
    const countsResult = {
      needs: 0,
      broken: 0,
      review: 0,
      working: 1,
      done: 0
    }
    const text = countsText(countsResult)
    expect(text).toBe('1 working')
  })

  it('returns empty string when all counts are zero', () => {
    const countsResult = {
      needs: 0,
      broken: 0,
      review: 0,
      working: 0,
      done: 0
    }
    const text = countsText(countsResult)
    expect(text).toBe('')
  })
})

describe('noteOutcome', () => {
  it('updates running run with matching id to succeeded when event is run_passed', () => {
    const runs = [run({ id: 10, state: 'running' })]
    const updatedRuns = noteOutcome(runs, 'run_passed', 10, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toHaveLength(1)
    expect(updatedRuns[0].state).toBe('succeeded')
    expect(updatedRuns[0].ended).toBe('2026-10-08T10:05:00Z')
  })

  it('updates running run to failed when event is run_failed', () => {
    const runs = [run({ id: 10, state: 'running' })]
    const updatedRuns = noteOutcome(runs, 'run_failed', 10, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toHaveLength(1)
    expect(updatedRuns[0].state).toBe('failed')
    expect(updatedRuns[0].ended).toBe('2026-10-08T10:05:00Z')
  })

  it('updates running run to interrupted when event is run_interrupted', () => {
    const runs = [run({ id: 10, state: 'running' })]
    const updatedRuns = noteOutcome(runs, 'run_interrupted', 10, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toHaveLength(1)
    expect(updatedRuns[0].state).toBe('interrupted')
    expect(updatedRuns[0].ended).toBe('2026-10-08T10:05:00Z')
  })

  it('does not change array when event is not run_passed, run_failed or run_interrupted', () => {
    const runs = [run({ id: 10, state: 'running' })]
    const updatedRuns = noteOutcome(runs, 'run_started', 10, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toBe(runs) // Should return same array object
  })

  it('does not change array when ref does not match run id', () => {
    const runs = [run({ id: 10, state: 'running' })]
    const updatedRuns = noteOutcome(runs, 'run_passed', 20, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toBe(runs) // Should return same array object
  })

  it('does not change array when run is not running', () => {
    const runs = [run({ id: 10, state: 'succeeded' })]
    const updatedRuns = noteOutcome(runs, 'run_passed', 10, '2026-10-08T10:05:00Z')
    
    expect(updatedRuns).toBe(runs) // Should return same array object
  })

  it('does not change array when event or ref is missing', () => {
    const runs = [run({ id: 10, state: 'running' })]
    
    // Missing event
    expect(noteOutcome(runs, '', 10, '2026-10-08T10:05:00Z')).toBe(runs)
    
    // Missing ref
    expect(noteOutcome(runs, 'run_passed', undefined, '2026-10-08T10:05:00Z')).toBe(runs)
  })
})