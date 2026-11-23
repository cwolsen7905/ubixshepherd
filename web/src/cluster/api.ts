// The cluster's calls: a hub's members, listed and managed from Settings.
import { get, post } from '../api/client'

export interface Member {
  name: string
  daemon_id?: string
  enrolled: boolean
  online: boolean
  /** When it came online, or, offline, when it was last seen; absent if it never linked. */
  since?: string
  version?: string
  api_level?: number
  capabilities?: string[]
  runs_going: number
  waiting: number
  /** A daemon id that differs from the one it enrolled with: refused until confirmed. */
  pending_id?: string
  /** When an unspent enrolment code ends. */
  code_expires?: string
}

export interface ClusterView {
  role: 'hub' | 'member' | 'alone'
  daemon_id: string
  members?: Member[]
}

export interface MemberCode {
  name: string
  code: string
  expires: string
}

const member = (name: string, act: string) => `/v1/cluster/members/${encodeURIComponent(name)}/${act}`

export const cluster = {
  get: () => get<ClusterView>('/v1/cluster'),
  add: (name: string) => post<MemberCode>('/v1/cluster/members', { name }),
  revoke: (name: string) => post<unknown>(member(name, 'revoke'), {}),
  remove: (name: string) => post<unknown>(member(name, 'remove'), {}),
  confirm: (name: string) => post<unknown>(member(name, 'confirm'), {}),
}
