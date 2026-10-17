// Drives headless Chrome over the DevTools protocol and writes one PNG per page and
// theme. Run by shots.sh, which starts the stub daemon and sets the environment.
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'

const { CHROME, SHOTS_BASE, SHOTS_OUT, SHOTS_W, SHOTS_H, SHOTS_SCALE, SHOTS_THEMES, SHOTS_PAGES } = process.env
const ALL_PAGES = [
  ['chat', '#/chat'],
  ['chat-next', '#/chat', { rail: 'Next up' }],
  ['next', '#/next'],
  // The blocked item's detail opens in the page.
  ['next-item', '#/next?item=2'],
  ['board', '#/board'],
  // The lane in the side panel (Overview, then Changes), and as a page.
  ['lane-panel', '#/board?open=lane%3A4'],
  ['lane-panel-changes', '#/board?open=lane%3A4/changes'],
  ['lane', '#/lanes/4'],
  // A third element names a tab to open and a file to expand on that page.
  ['lane-changes', '#/lanes/4', { tab: 'Changes', file: 'internal/auth/session.go' }],
  ['lane-files', '#/lanes/4', { tab: 'Files', click: [['README.md', '.sh-file-tree-item']] }],
  ['lane-artifacts', '#/lanes/4', { tab: 'Artifacts' }],
  ['lane-runs', '#/lanes/4', { tab: 'Runs' }],
  // The same pages scrolled well down: the page header must stay fully in view.
  ['lane-changes-scrolled', '#/lanes/4', { tab: 'Changes', file: 'internal/auth/session.go', scroll: 600 }],
  ['lane-runs-scrolled', '#/lanes/4', { tab: 'Runs', scroll: 600 }],
  ['run-log-scrolled', '#/runs/17/log', { scroll: 600 }],
  ['decision-page-scrolled', '#/inbox?open=decision%3A5/full', { scroll: 600 }],
  ['mr-page-scrolled', '#/inbox?open=mr%3A1/changes/full', { scroll: 600 }],
  ['artifact-page-scrolled', '#/artifacts?open=artifact%3A3/full', { scroll: 600 }],
  ['artifact-view', '#/artifacts?view=1'],
  ['artifact-view-scrolled', '#/artifacts?view=1', { scroll: 600 }],
  ['run-page-scrolled', '#/runs?open=run%3A17/full', { scroll: 600 }],
  ['files-scrolled', '#/files?lane=4', { click: [['README.md', '.sh-file-tree-item']], scroll: 600 }],
  ['lane-closed', '#/lanes/7'],
  ['run-log', '#/runs/17/log'],
  ['decisions', '#/decisions'],
  ['timeline', '#/timeline'],
  // The Conversation: the thread, a decision swapped into the main column, and the return card.
  ['conversation', '#/'],
  ['conversation-work', '#/', { click: [['Show work', 'summary']] }],
  ['conversation-away', '#/', { top: true, storage: () => ({ 'shepherd.seen.v1': JSON.stringify({ seen: [], looked: Date.now() - 3 * 3600_000, notify: false }) }) }],
  ['conversation-decision', '#/?open=decision%3A5'],
  // The Inbox, a merge request in the side panel and as a page, and a decision in the main body.
  // Done today is read once the workspaces are known, and a second read waits 15 seconds.
  ['inbox', '#/inbox', { wait: 16000 }],
  ['inbox-mr', '#/inbox?open=mr%3A1', { wait: 16000 }],
  ['inbox-mr-changes', '#/inbox?open=mr%3A1/changes'],
  ['inbox-mr-page', '#/inbox?open=mr%3A1/full'],
  ['inbox-decision', '#/inbox?open=decision%3A5'],
  ['decision-page', '#/inbox?open=decision%3A5/full'],
  // Artifacts: the list, a plan kept local, an image, a committed code file, an undecided one.
  ['artifacts', '#/artifacts'],
  ['artifact-plan', '#/artifacts?open=artifact%3A1'],
  ['artifact-page', '#/artifacts?open=artifact%3A1/full'],
  ['artifact-image', '#/artifacts?open=artifact%3A2/full'],
  ['artifact-code', '#/artifacts?open=artifact%3A3/full'],
  ['artifact-undecided', '#/artifacts?open=artifact%3A4'],
  ['files', '#/files?lane=4', { click: [['README.md', '.sh-file-tree-item']] }],
  ['files-tree', '#/files?lane=1', { click: [['internal', '.sh-file-tree-item'], ['auth', '.sh-file-tree-item'], ['session.go', '.sh-file-tree-item']] }],
  ['runs', '#/runs'],
  ['run-panel', '#/runs?open=run%3A17'],
  ['run-page', '#/runs?open=run%3A17/full'],
  ['run-failed', '#/runs?open=run%3A14'],
  ['history', '#/history'],
  ['projects', '#/projects'],
  ['project', '#/projects/acme'],
  ['rules', '#/rules'],
  ['usage', '#/usage'],
  ['settings', '#/settings'],
  ['status', '#/status'],
]
// SHOTS_PAGES names the pages to take ("timeline board"); by default, all of them.
const only = (SHOTS_PAGES ?? '').split(/\s+/).filter(Boolean)
const PAGES = only.length ? ALL_PAGES.filter(([name]) => only.includes(name)) : ALL_PAGES
const unknown = only.filter((n) => !ALL_PAGES.some(([name]) => name === n))
if (unknown.length) throw new Error(`no such page: ${unknown.join(' ')}`)
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
  // A page may start with things in localStorage (the seen record, to show the away card).
  if (steps?.storage) {
    const items = JSON.stringify(steps.storage())
    await s('Page.enable')
    await s('Page.addScriptToEvaluateOnNewDocument', { source: `for (const [k, v] of Object.entries(${items})) localStorage.setItem(k, v)` })
  }
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
  if (steps?.wait) await sleep(steps.wait)
  if (steps) {
    const click = (text, role) => s('Runtime.evaluate', { expression: `(() => { const el = [...document.querySelectorAll('${role}')].find((e) => e.textContent.includes(${JSON.stringify(text)})); el?.click(); return !!el })()`, returnByValue: true })
    if (steps.rail) await click(steps.rail, '.backlog-views button')
    if (steps.tab) {
      await click(steps.tab, '[role=tab]')
      await sleep(800)
    }
    for (const [text, selector] of steps.click ?? []) {
      await click(text, selector)
      await sleep(400)
    }
    if (steps.top) await s('Runtime.evaluate', { expression: "document.querySelector('[data-component=Thread]').scrollTop = 0" })
    await sleep(800)
    if (steps.file) await click(steps.file, '.change-toggle')
    await sleep(800)
    if (steps.scroll) {
      // The window, and any column that scrolls itself (a full-width inspector).
      await s('Runtime.evaluate', {
        expression: `(() => { window.scrollTo(0, ${+steps.scroll}); for (const el of document.querySelectorAll('*')) if (el.scrollHeight > el.clientHeight + 50 && /auto|scroll/.test(getComputedStyle(el).overflowY)) el.scrollTop = ${+steps.scroll} })()`,
      })
      await sleep(500)
    }
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
