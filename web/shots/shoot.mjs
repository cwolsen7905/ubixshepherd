// Drives headless Chrome over the DevTools protocol and writes one PNG per page and
// theme. Run by shots.sh, which starts the stub daemon and sets the environment.
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

const { CHROME, SHOTS_BASE, SHOTS_OUT, SHOTS_W, SHOTS_H, SHOTS_SCALE, SHOTS_THEMES, SHOTS_PAGES } = process.env
const ALL_PAGES = [
  ['chat', '#/chat'],
  ['board', '#/'],
  ['lane', '#/lanes/4'],
  // A third element names a tab to open and a file to expand on that page.
  ['lane-changes', '#/lanes/4', { tab: 'Changes', file: 'internal/auth/session.go' }],
  ['run-log', '#/runs/17/log'],
  ['decisions', '#/decisions'],
  ['timeline', '#/timeline'],
]
// SHOTS_PAGES names the pages to take ("timeline board"); by default, all of them.
const only = (SHOTS_PAGES ?? '').split(/\s+/).filter(Boolean)
const PAGES = only.length ? ALL_PAGES.filter(([name]) => only.includes(name)) : ALL_PAGES
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const profile = fs.mkdtempSync(path.join(os.tmpdir(), 'shots-chrome-'))
const chrome = spawn(CHROME, [
  '--headless=new', '--disable-gpu', '--hide-scrollbars', '--no-first-run', '--no-default-browser-check',
  `--user-data-dir=${profile}`, '--remote-debugging-port=0', 'about:blank',
], { stdio: ['ignore', 'ignore', 'pipe'] })

const wsUrl = await new Promise((resolve, reject) => {
  let buf = ''
  chrome.stderr.on('data', (d) => {
    buf += d
    const m = buf.match(/DevTools listening on (ws:\/\/\S+)/)
    if (m) resolve(m[1])
  })
  chrome.on('exit', () => reject(new Error('chrome exited before it was ready')))
  setTimeout(() => reject(new Error('chrome did not start in 20s')), 20000)
})

const ws = new WebSocket(wsUrl)
await new Promise((r, j) => { ws.onopen = r; ws.onerror = () => j(new Error('devtools connection failed')) })
let nextId = 1
const pending = new Map()
ws.onmessage = (ev) => {
  const m = JSON.parse(ev.data)
  const p = pending.get(m.id)
  if (!p) return
  pending.delete(m.id)
  m.error ? p.reject(new Error(m.error.message)) : p.resolve(m.result)
}
const send = (method, params = {}, sessionId) => new Promise((resolve, reject) => {
  const id = nextId++
  pending.set(id, { resolve, reject })
  ws.send(JSON.stringify({ id, method, params, ...(sessionId ? { sessionId } : {}) }))
})

async function shot(name, route, theme, steps) {
  const { targetId } = await send('Target.createTarget', { url: 'about:blank' })
  const { sessionId } = await send('Target.attachToTarget', { targetId, flatten: true })
  const s = (m, p) => send(m, p, sessionId)
  await s('Emulation.setDeviceMetricsOverride', { width: +SHOTS_W, height: +SHOTS_H, deviceScaleFactor: +SHOTS_SCALE, mobile: false })
  // The pulse and the breathing are frozen, so a shot repeats: the timeline's static markers
  // are drawn for that.
  await s('Emulation.setEmulatedMedia', {
    features: [{ name: 'prefers-color-scheme', value: theme }, { name: 'prefers-reduced-motion', value: 'reduce' }],
  })
  await s('Page.navigate', { url: `${SHOTS_BASE}/${route}` })
  // Ready when the lists and the thread have loaded; a page that never settles is taken as it is.
  const settled = 'document.body.innerText.length > 80 && !/Connecting|Loading|not ready/i.test(document.body.innerText)'
  const until = Date.now() + 15000
  while (Date.now() < until) {
    const r = await s('Runtime.evaluate', { expression: settled, returnByValue: true })
    if (r.result?.value) break
    await sleep(250)
  }
  await sleep(1500) // the stub's streamed events arrive over about two seconds
  if (steps) {
    const click = (text, role) => s('Runtime.evaluate', { expression: `(() => { const el = [...document.querySelectorAll('${role}')].find((e) => e.textContent.includes(${JSON.stringify(text)})); el?.click(); return !!el })()`, returnByValue: true })
    await click(steps.tab, '[role=tab]')
    await sleep(800)
    if (steps.file) await click(steps.file, '.change-toggle')
    await sleep(800)
  }
  const { data } = await s('Page.captureScreenshot', { format: 'png' })
  const file = path.join(SHOTS_OUT, `${name}-${theme}.png`)
  fs.writeFileSync(file, Buffer.from(data, 'base64'))
  await send('Target.closeTarget', { targetId })
  console.log(file)
}

let failed = false
try {
  for (const theme of SHOTS_THEMES.split(/\s+/).filter(Boolean)) {
    for (const [name, route, steps] of PAGES) await shot(name, route, theme, steps)
  }
} catch (e) {
  console.error(`shots: ${e.message}`)
  failed = true
} finally {
  ws.close()
  chrome.kill()
  await sleep(300)
  fs.rmSync(profile, { recursive: true, force: true })
}
process.exit(failed ? 1 : 0)
