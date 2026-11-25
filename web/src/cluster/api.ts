// The cluster's calls: a controller's workers, listed and managed from Settings.
import { get, post } from '../api/client'

export interface Worker {
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
  role: 'controller' | 'worker' | 'alone'
  daemon_id: string
  workers?: Worker[]
}

export interface WorkerCode {
  name: string
  code: string
  expires: string
}

const worker = (name: string, act: string) => `/v1/cluster/workers/${encodeURIComponent(name)}/${act}`

export const cluster = {
  get: () => get<ClusterView>('/v1/cluster'),
  add: (name: string) => post<WorkerCode>('/v1/cluster/workers', { name }),
  revoke: (name: string) => post<unknown>(worker(name, 'revoke'), {}),
  remove: (name: string) => post<unknown>(worker(name, 'remove'), {}),
  confirm: (name: string) => post<unknown>(worker(name, 'confirm'), {}),
}
