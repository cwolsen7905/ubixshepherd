import { render, screen } from '@testing-library/react'
import { Legend } from './Legend'

describe('Legend', () => {
  it('renders one dd per event', () => {
    render(<Legend />)
    
    // There should be 22 events
    const dds = screen.getAllByRole('definition')
    expect(dds).toHaveLength(22)
  })
  
  it('contains "run passed" and "request needs you"', () => {
    render(<Legend />)
    
    // Use queryAllByText to get all elements with the text, then check if they're in dd elements
    const runPassed = screen.queryByText('run passed')
    const requestNeedsYou = screen.queryByText('request needs you')
    
    expect(runPassed).toBeInTheDocument()
    expect(requestNeedsYou).toBeInTheDocument()
    
    // Verify these are actually in dd elements (not in visually hidden spans)
    expect(runPassed?.closest('dd')).toBeInTheDocument()
    expect(requestNeedsYou?.closest('dd')).toBeInTheDocument()
  })
})