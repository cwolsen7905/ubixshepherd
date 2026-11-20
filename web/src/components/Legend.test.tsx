import { render, screen } from '@testing-library/react'
import { GLYPHS } from '../lib/keys'
import { Legend } from './Legend'

describe('Legend', () => {
  it('renders one dd per event', () => {
    render(<Legend />)
    
    // Ten status glyphs, then the 26 feed events
    const dds = screen.getAllByRole('definition')
    expect(dds).toHaveLength(10 + 26)
  })
  
  it('says what each glyph means in its own words', () => {
    render(<Legend />)
    const meanings = screen.getAllByRole('definition').map((dd) => dd.textContent)
    expect(meanings).toContain('run passed')
    expect(meanings).toContain('request needs you')
  })

  it('lists every status glyph with its words', () => {
    render(<Legend />)
    const meanings = screen.getAllByRole('definition').map((dd) => dd.textContent)
    for (const g of GLYPHS) expect(meanings).toContain(g.label)
  })
})
