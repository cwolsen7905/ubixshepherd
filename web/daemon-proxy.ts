// The dev server's link to a running daemon: /v1 is forwarded to it with its token. The
// daemon writes its address and token to its runtime file (daemon.json in SHEPHERD_HOME,
// ~/.shepherd by default) and rewrites it when it restarts, so the file is read again on
// every request. The token only goes into the forwarded request's header: never print,
// log or bundle it.
import { readFileSync } from 'node:fs'
import { request, type IncomingMessage, type ServerResponse } from 'node:http'
import { homedir } from 'node:os'
import { join } from 'node:path'
import type { Plugin } from 'vite'

interface Runtime {
  addr: string
  token: string
}

function readRuntime(): Runtime | null {
  const path = join(process.env.SHEPHERD_HOME || join(homedir(), '.shepherd'), 'daemon.json')
  try {
    const rt = JSON.parse(readFileSync(path, 'utf8')) as Partial<Runtime>
    return rt.addr && rt.token ? { addr: rt.addr, token: rt.token } : null
  } catch {
    return null
  }
}

function fail(res: ServerResponse, error: string) {
  if (res.headersSent) return res.end()
  res.writeHead(502, { 'Content-Type': 'application/json' })
  res.end(JSON.stringify({ error }))
}

function forward(req: IncomingMessage, res: ServerResponse) {
  const rt = readRuntime()
  if (!rt) return fail(res, 'no daemon is running; start one with `shepherd daemon start`')
  const [host, port] = splitAddr(rt.addr)
  const headers = { ...req.headers }
  delete headers.host
  delete headers.cookie
  const up = request(
    {
      host, port, method: req.method, path: req.url,
      headers: { ...headers, authorization: 'Bearer ' + rt.token, 'x-shepherd-client': 'web' },
    },
    (upRes) => {
      res.writeHead(upRes.statusCode ?? 502, upRes.headers)
      upRes.pipe(res)
    },
  )
  // Say what is wrong without echoing the runtime file.
  up.on('error', () => fail(res, 'the daemon is not reachable; is it running?'))
  req.pipe(up)
}

function splitAddr(addr: string): [string, number] {
  const i = addr.lastIndexOf(':')
  const host = addr.slice(0, i).replace(/^\[|\]$/g, '') || '127.0.0.1'
  return [host, Number(addr.slice(i + 1))]
}

export function daemonProxy(): Plugin {
  return {
    name: 'shepherd-daemon-proxy',
    configureServer(server) {
      server.middlewares.use((req, res, next) => {
        if (!req.url?.startsWith('/v1/')) return next()
        forward(req, res)
      })
    },
  }
}
