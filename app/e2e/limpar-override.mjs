import { _electron as electron } from 'playwright'

const app = await electron.launch({ args: ['.'], cwd: process.cwd() })
const w = await app.firstWindow()
await w.waitForSelector('text=Artigos Ana', { timeout: 20000 })
const antes = await w.evaluate(() => localStorage.getItem('artigosAnaApiBase'))
await w.evaluate(() => localStorage.removeItem('artigosAnaApiBase'))
const depois = await w.evaluate(() => localStorage.getItem('artigosAnaApiBase'))
console.log('override antes:', antes, '| depois:', depois)
await app.close()
