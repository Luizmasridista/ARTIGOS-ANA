import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import fs from 'node:fs'
import os from 'node:os'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const dirname = path.dirname(fileURLToPath(import.meta.url))
const appDir = path.resolve(dirname, '..')
const backendDir = path.resolve(appDir, '..', 'backend')
const API = process.env.E2E_API || 'http://127.0.0.1:8735'
const E2E_DATA = path.join(os.tmpdir(), 'ana-e2e-home-data')

const wait = (ms) => new Promise((r) => setTimeout(r, ms))

async function healthOk() {
  try {
    const r = await fetch(`${API}/api/health`, { signal: AbortSignal.timeout(2000) })
    return r.ok
  } catch {
    return false
  }
}

async function subirBackend() {
  const exe = path.join(backendDir, 'bin', 'artigos-ana.exe')
  if (!fs.existsSync(exe)) {
    console.log('backend bin não encontrado, tentando go run')
    const out = fs.openSync(path.join(os.tmpdir(), 'ana-e2e-home.log'), 'a')
    const p = spawn('go', ['run', '.', '-port', '8735', '-data', E2E_DATA, '-www', ''], {
      cwd: backendDir,
      windowsHide: true,
      stdio: ['ignore', out, out],
      env: { ...process.env, ALLOW_INSECURE_COOKIE: '1' },
    })
    p.unref()
    return
  }
  const out = fs.openSync(path.join(os.tmpdir(), 'ana-e2e-home.log'), 'a')
  const p = spawn(exe, ['-port', '8735', '-data', E2E_DATA, '-www', ''], {
    cwd: backendDir,
    windowsHide: true,
    stdio: ['ignore', out, out],
    env: { ...process.env, ALLOW_INSECURE_COOKIE: '1' },
  })
  p.unref()
}

async function garantirBackend() {
  for (let tentativa = 0; tentativa < 6; tentativa++) {
    if (await healthOk()) return
    await subirBackend()
    for (let i = 0; i < 40 && !(await healthOk()); i++) await wait(300)
  }
  throw new Error('backend e2e home-auth não subiu')
}

function parseSetCookie(header) {
  if (!header) return null
  // Node fetch may combine multiple Set-Cookie? Use getSetCookie if available
  return header
}

async function login(nome, senha, ip) {
  const headers = { 'Content-Type': 'application/json' }
  if (ip) headers['X-Forwarded-For'] = ip
  const res = await fetch(`${API}/api/auth/login`, {
    method: 'POST',
    headers,
    body: JSON.stringify({ nome, senha }),
  })
  const text = await res.text()
  let body
  try { body = JSON.parse(text) } catch { body = text }
  // @ts-ignore getSetCookie is Node fetch
  const setCookie = res.headers.getSetCookie ? res.headers.getSetCookie() : res.headers.get('set-cookie')
  return { res, body, setCookie }
}

async function me(cookie, ip) {
  const headers = {}
  if (cookie) headers['Cookie'] = cookie
  if (ip) headers['X-Forwarded-For'] = ip
  const res = await fetch(`${API}/api/auth/me`, { headers })
  const body = await res.json().catch(() => null)
  return { res, body }
}

async function artigos(cookie, ip) {
  const headers = {}
  if (cookie) headers['Cookie'] = cookie
  if (ip) headers['X-Forwarded-For'] = ip
  const res = await fetch(`${API}/api/artigos`, { headers })
  return res
}

console.log('E2E home-auth: garantindo backend', API)
await garantirBackend()
console.log('backend ok')

// 1. login 200 com cookie HttpOnly/Secure/SameSite
console.log('1. login 200')
const ipOk = `10.10.1.${Date.now() % 200}`
const ok = await login('Ana Bagatinii', 'ana123', ipOk)
assert.equal(ok.res.status, 200, `login 200 esperado veio ${ok.res.status} ${JSON.stringify(ok.body)}`)
const sc = Array.isArray(ok.setCookie) ? ok.setCookie.join('; ') : String(ok.setCookie || '')
console.log('Set-Cookie:', sc.slice(0, 200))
assert.ok(sc.includes('ana_session='), 'Set-Cookie deve conter ana_session')
assert.ok(sc.includes('HttpOnly'), 'cookie deve ser HttpOnly')
assert.ok(sc.includes('SameSite=Strict'), 'cookie deve ser SameSite=Strict')
assert.ok(sc.includes('Path=/'), 'cookie Path=/')
assert.ok(sc.includes('Max-Age=3600'), 'Max-Age 3600')
const cookieVal = (() => {
  const m = sc.match(/ana_session=([^;]+)/)
  return m ? `ana_session=${m[1]}` : ''
})()
assert.ok(cookieVal, 'não extraiu ana_session')
assert.ok(cookieVal.split('=')[1].split('.').length === 3, 'JWT deve ter 3 partes')

// 2. login 401
console.log('2. login 401')
const ip401 = `10.10.2.${Date.now() % 200}`
const bad = await login('Ana Bagatinii', 'senha_errada_xxx', ip401)
assert.equal(bad.res.status, 401, `401 esperado veio ${bad.res.status}`)
assert.equal(bad.body.erro, 'credenciais inválidas')
const scBad = Array.isArray(bad.setCookie) ? bad.setCookie.join('; ') : String(bad.setCookie || '')
assert.ok(!scBad.includes('ana_session=') || scBad.includes('Max-Age=0') || scBad === 'null' || scBad === '', '401 não deve setar cookie válido')

// 3. brute force 6ª 423
console.log('3. brute force 5x401 + 6ª 423')
const ipBrute = `9.9.${Date.now() % 250}.${Date.now() % 250}`
for (let i = 0; i < 5; i++) {
  const r = await login('Ana Bagatinii', 'errada', ipBrute)
  assert.equal(r.res.status, 401, `tentativa ${i + 1} deveria 401 veio ${r.res.status}`)
}
const locked = await login('Ana Bagatinii', 'errada', ipBrute)
assert.equal(locked.res.status, 423, `6ª deveria 423 veio ${locked.res.status} ${JSON.stringify(locked.body)}`)
assert.equal(locked.body.erro, 'muitas tentativas')
assert.equal(locked.body.retryAfter, 900)
assert.equal(locked.res.headers.get('retry-after'), '900')
// mesmo com senha correta ainda 423
const lockedOk = await login('Ana Bagatinii', 'ana123', ipBrute)
assert.equal(lockedOk.res.status, 423, 'mesmo senha correta bloqueado deveria 423')

// 4. sessão persiste via cookie: me 200 e artigos 200 com cookie, 401 sem
console.log('4. sessão persiste e 401 sem cookie')
const meOk = await me(cookieVal)
assert.equal(meOk.res.status, 200, `me com cookie deveria 200 veio ${meOk.res.status}`)
assert.equal(meOk.body.nome, 'Ana Bagatinii')
const meNo = await me('')
assert.equal(meNo.res.status, 401, 'me sem cookie 401')
const artNo = await artigos('')
assert.equal(artNo.status, 401, 'GET /api/artigos sem cookie 401')
const artOk = await artigos(cookieVal)
assert.ok([200, 204].includes(artOk.status) || artOk.ok, `GET /api/artigos com cookie deveria ok veio ${artOk.status}`)

// 5. logout limpa cookie
console.log('5. logout')
const logoutRes = await fetch(`${API}/api/auth/logout`, { method: 'POST', headers: { Cookie: cookieVal } })
assert.equal(logoutRes.status, 204)
const scOut = logoutRes.headers.getSetCookie ? logoutRes.headers.getSetCookie().join('; ') : (logoutRes.headers.get('set-cookie') || '')
assert.ok(scOut.includes('ana_session='), 'logout Set-Cookie ana_session')
assert.ok(scOut.includes('Max-Age=0') || scOut.includes('Max-Age=-1') || scOut.includes('Expires=Thu, 01 Jan 1970'), 'logout deve expirar cookie')

// 6. headers HSTS e CORS
console.log('6. headers HSTS e CORS')
const healthPlain = await fetch(`${API}/api/health`)
assert.equal(healthPlain.headers.get('x-frame-options'), 'DENY')
assert.equal(healthPlain.headers.get('x-content-type-options'), 'nosniff')
assert.equal(healthPlain.headers.get('referrer-policy'), 'no-referrer')
assert.ok((healthPlain.headers.get('content-security-policy') || '').includes("default-src 'self'"))
assert.equal(healthPlain.headers.get('strict-transport-security') || '', '', 'HSTS sem https deve vazio')
const healthHttps = await fetch(`${API}/api/health`, { headers: { 'X-Forwarded-Proto': 'https' } })
assert.equal(healthHttps.headers.get('strict-transport-security'), 'max-age=31536000; includeSubDomains')
// CORS
const corsAllow = await fetch(`${API}/api/health`, { headers: { Origin: 'http://127.0.0.1:5173' } })
assert.equal(corsAllow.headers.get('access-control-allow-origin'), 'http://127.0.0.1:5173')
assert.equal(corsAllow.headers.get('access-control-allow-credentials'), 'true')
const corsEvil = await fetch(`${API}/api/health`, { headers: { Origin: 'https://evil.com' } })
assert.equal(corsEvil.headers.get('access-control-allow-origin') || '', '', 'CORS evil deve vazio')

console.log('E2E home-auth OK: login 200/401/423, cookie HttpOnly/SameSite, sessão persiste, 401 redireciona, HSTS e CORS')
