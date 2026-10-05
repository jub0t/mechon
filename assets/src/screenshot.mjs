// Screenshot a URL through the Chrome DevTools Protocol (Node 22+ has a global WebSocket).
import { spawn } from 'node:child_process'
import { writeFileSync } from 'node:fs'
const [url, out, w = '1600', h = '940', waitMs = '3500', mx, my] = process.argv.slice(2)
const port = 9333
const chrome = spawn('/Applications/Google Chrome.app/Contents/MacOS/Google Chrome', [
  '--headless=new', `--remote-debugging-port=${port}`, `--user-data-dir=${process.env.TMPDIR || '/tmp'}/mechon-shot-chrome`,
  '--hide-scrollbars', `--window-size=${w},${h}`, 'about:blank'], { stdio: 'ignore' })
const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
let target
for (let i = 0; i < 50 && !target; i++) {
  await sleep(200)
  try { target = (await (await fetch(`http://127.0.0.1:${port}/json/list`)).json()).find((t) => t.type === 'page') } catch {}
}
const ws = new WebSocket(target.webSocketDebuggerUrl)
await new Promise((r) => ws.addEventListener('open', r))
let id = 0
const pending = new Map()
ws.addEventListener('message', (e) => { const m = JSON.parse(e.data); if (m.id && pending.has(m.id)) { pending.get(m.id)(m); pending.delete(m.id) } })
const send = (method, params = {}) => new Promise((r) => { const i = ++id; pending.set(i, r); ws.send(JSON.stringify({ id: i, method, params })) })
await send('Emulation.setDeviceMetricsOverride', { width: +w, height: +h, deviceScaleFactor: 2, mobile: false })
await send('Page.enable')
if (process.env.LOGIN) {
  await send('Runtime.enable')
  await send('Page.navigate', { url: new URL('/login', url).href })
  await sleep(1500)
  // Reuse the profile's session; sign in only when there is none (sign-in is rate-limited).
  const me = await send('Runtime.evaluate', { awaitPromise: true, expression: `fetch('/api/v1/me').then(r=>r.status)` })
  if (me.result?.result?.value !== 200)
    await send('Runtime.evaluate', { awaitPromise: true, expression: `fetch('/api/v1/auth/login',{method:'POST',headers:{'Content-Type':'application/json'},body:JSON.stringify({email:'${process.env.EMAIL || 'admin@mechon.test'}',password:'${process.env.PASSWORD || 'dev-password-123'}'})}).then(r=>r.status)` })
}
if (process.env.PRESET) await send('Runtime.evaluate', { expression: process.env.PRESET })
await send('Page.navigate', { url })
await sleep(1500)
if (mx) { await sleep(1500); await send('Input.dispatchMouseEvent', { type: 'mouseMoved', x: +mx, y: +my }) }
await sleep(+waitMs)
const shot = await send('Page.captureScreenshot', { format: 'png' })
writeFileSync(out, Buffer.from(shot.result.data, 'base64'))
ws.close(); chrome.kill()
console.log('saved', out)
