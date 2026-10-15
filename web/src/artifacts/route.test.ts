import { href, parse } from '../router'

describe('artifacts route', () => {
  it('round-trips its filters and drops bad ones', () => {
    expect(parse(href.artifacts())).toEqual({ page: 'artifacts' })
    expect(parse(href.artifacts({ repo: 2, lane: 4, run: 17, topic: 3 }))).toEqual({ page: 'artifacts', repo: 2, lane: 4, run: 17, topic: 3 })
    expect(parse('#/artifacts?lane=x&run=-1')).toEqual({ page: 'artifacts' })
    expect(parse('#/artifacts?open=artifact:12')).toEqual({ page: 'artifacts', open: { kind: 'artifact', id: 12 } })
  })
})
