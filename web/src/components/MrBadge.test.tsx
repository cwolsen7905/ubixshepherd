import { render, screen } from '@testing-library/react'
import { mrBadge, MrBadge } from './MrBadge'
import type { LaneView } from '../api/types'

describe('mrBadge', () => {
  describe('returns null when mr is undefined or 0', () => {
    it('returns null when mr is undefined', () => {
      const lane = {
        mr: undefined,
        mr_state: 'open',
        pipeline_status: 'passed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toBeNull()
    })

    it('returns null when mr is 0', () => {
      const lane = {
        mr: 0,
        mr_state: 'open',
        pipeline_status: 'passed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toBeNull()
    })
  })

  describe('returns correct badge when mr_state is merged', () => {
    it('returns merged badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'merged',
        pipeline_status: 'passed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 merged',
        tone: 'ok',
        label: 'merge request !34, merged'
      })
    })
  })

  describe('returns correct badge when mr_state is closed', () => {
    it('returns closed badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'closed',
        pipeline_status: 'passed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 closed',
        tone: 'muted',
        label: 'merge request !34, closed'
      })
    })
  })

  describe('returns correct badge when pipeline_status is passed', () => {
    it('returns passed badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'passed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 ✓',
        tone: 'ok',
        label: 'merge request !34, pipeline passed'
      })
    })
  })

  describe('returns correct badge when pipeline_status is failed', () => {
    it('returns failed badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'failed'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 ✗',
        tone: 'bad',
        label: 'merge request !34, pipeline failed'
      })
    })
  })

  describe('returns correct badge when pipeline_status is pending', () => {
    it('returns pending badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'pending'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 …',
        tone: 'muted',
        label: 'merge request !34, pipeline pending'
      })
    })
  })

  describe('returns correct badge when pipeline_status is running', () => {
    it('returns running badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'running'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 …',
        tone: 'muted',
        label: 'merge request !34, pipeline running'
      })
    })
  })

  describe('returns correct badge when pipeline_status is canceled', () => {
    it('returns canceled badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'canceled'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 canceled',
        tone: 'muted',
        label: 'merge request !34, pipeline canceled'
      })
    })
  })

  describe('returns correct badge when pipeline_status is skipped', () => {
    it('returns skipped badge', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'skipped'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34 skipped',
        tone: 'muted',
        label: 'merge request !34, pipeline skipped'
      })
    })
  })

  describe('returns correct badge for other pipeline_status values', () => {
    it('returns muted badge for unknown pipeline_status', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'unknown'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34',
        tone: 'muted',
        label: 'merge request !34'
      })
    })

    it('returns muted badge when no pipeline_status', () => {
      const lane = {
        mr: 34,
        mr_state: 'open'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status'>
      
      expect(mrBadge(lane)).toEqual({
        text: '!34',
        tone: 'muted',
        label: 'merge request !34'
      })
    })
  })
})

describe('MrBadge component', () => {
  describe('renders nothing when mr is undefined or 0', () => {
    it('renders nothing when mr is undefined', () => {
      const lane = {
        mr: undefined,
        mr_state: 'open',
        pipeline_status: 'passed',
        mr_url: 'https://example.com'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      const { container } = render(<MrBadge lane={lane} />)
      expect(container).toBeEmptyDOMElement()
    })

    it('renders nothing when mr is 0', () => {
      const lane = {
        mr: 0,
        mr_state: 'open',
        pipeline_status: 'passed',
        mr_url: 'https://example.com'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      const { container } = render(<MrBadge lane={lane} />)
      expect(container).toBeEmptyDOMElement()
    })
  })

  describe('renders a link when mr_url is set', () => {
    it('renders a link with correct href, text and class', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'passed',
        mr_url: 'https://example.com/34'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      render(<MrBadge lane={lane} />)
      
      const link = screen.getByRole('link')
      expect(link).toHaveAttribute('href', 'https://example.com/34')
      expect(link).toHaveTextContent('!34 ✓')
      expect(link).toHaveClass('badge', 'tone-ok')
      expect(link).toHaveAttribute('target', '_blank')
      expect(link).toHaveAttribute('rel', 'noreferrer')
      expect(link).toHaveAttribute('title', 'merge request !34, pipeline passed')
      expect(link).toHaveAttribute('aria-label', 'merge request !34, pipeline passed')
    })
  })

  describe('renders a span when mr_url is not set', () => {
    it('renders a span with correct text and class', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'passed',
        mr_url: undefined
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      render(<MrBadge lane={lane} />)
      
      const span = screen.getByText('!34 ✓')
      expect(span).toHaveClass('badge', 'tone-ok')
      expect(span).toHaveAttribute('title', 'merge request !34, pipeline passed')
      expect(span).toHaveAttribute('aria-label', 'merge request !34, pipeline passed')
    })
  })

  it('renders a span, not a link, when link is false', () => {
    render(<MrBadge lane={{ mr: 34, pipeline_status: 'failed', mr_url: 'https://example.com/34' }} link={false} />)
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
    expect(screen.getByText('!34 ✗')).toHaveClass('badge', 'tone-bad')
  })

  describe('renders correct badge based on mr_state and pipeline_status', () => {
    it('renders merged badge as link', () => {
      const lane = {
        mr: 34,
        mr_state: 'merged',
        pipeline_status: 'passed',
        mr_url: 'https://example.com/34'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      render(<MrBadge lane={lane} />)
      
      const link = screen.getByRole('link')
      expect(link).toHaveTextContent('!34 merged')
      expect(link).toHaveClass('badge', 'tone-ok')
      expect(link).toHaveAttribute('title', 'merge request !34, merged')
    })

    it('renders closed badge as link', () => {
      const lane = {
        mr: 34,
        mr_state: 'closed',
        pipeline_status: 'passed',
        mr_url: 'https://example.com/34'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      render(<MrBadge lane={lane} />)
      
      const link = screen.getByRole('link')
      expect(link).toHaveTextContent('!34 closed')
      expect(link).toHaveClass('badge', 'tone-muted')
      expect(link).toHaveAttribute('title', 'merge request !34, closed')
    })

    it('renders failed badge as link', () => {
      const lane = {
        mr: 34,
        mr_state: 'open',
        pipeline_status: 'failed',
        mr_url: 'https://example.com/34'
      } as Pick<LaneView, 'mr' | 'mr_state' | 'pipeline_status' | 'mr_url'>
      
      render(<MrBadge lane={lane} />)
      
      const link = screen.getByRole('link')
      expect(link).toHaveTextContent('!34 ✗')
      expect(link).toHaveClass('badge', 'tone-bad')
      expect(link).toHaveAttribute('title', 'merge request !34, pipeline failed')
    })
  })
})