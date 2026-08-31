import { app, BrowserWindow, ipcMain, shell } from 'electron'
import { spawn, type ChildProcess } from 'child_process'
import * as fs from 'fs'
import * as path from 'path'

const API_BASE = 'http://127.0.0.1:8734'
let backendProcess: ChildProcess | null = null
let grokProcess: ChildProcess | null = null
let grokUrl: string | null = null
let grokStatus: 'off' | 'starting' | 'online' | 'error' = 'off'
let grokError: string | null = null

function backendDir(): string {
  return path.resolve(app.getAppPath(), '..', 'backend')
}

function startBackend(): void {
  const dir = backendDir()
  const exePath = path.join(dir, 'bin', 'artigos-ana.exe')
  try {
    if (fs.existsSync(exePath)) {
      backendProcess = spawn(exePath, [], { cwd: dir, windowsHide: true, stdio: 'ignore' })
    } else {
      backendProcess = spawn('go', ['run', '.'], { cwd: dir, windowsHide: true, stdio: 'ignore' })
    }
  } catch {
    backendProcess = null
  }
  if (backendProcess) {
    backendProcess.on('error', () => {
      backendProcess = null
    })
    backendProcess.on('exit', () => {
      backendProcess = null
    })
  }
}

function stopBackend(): void {
  if (!backendProcess || !backendProcess.pid) return
  try {
    if (process.platform === 'win32') {
      spawn('taskkill', ['/pid', String(backendProcess.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' })
    } else {
      backendProcess.kill()
    }
  } catch {
    // processo já encerrado
  }
  backendProcess = null
}

function findCloudflared(): string | null {
  const candidates = [
    path.join(process.env.LOCALAPPDATA || '', 'Microsoft', 'WinGet', 'Packages', 'Cloudflare.cloudflared_Microsoft.Winget.Source_8wekyb3d8bbwe', 'cloudflared.exe'),
    'C:\\Program Files\\Cloudflare\\cloudflared.exe',
    path.join(backendDir(), '..', 'bin', 'cloudflared.exe'),
  ]
  for (const c of candidates) {
    try {
      if (c && fs.existsSync(c)) return c
    } catch {}
  }
  // tenta no PATH
  try {
    const which = spawn('where', ['cloudflared'], { windowsHide: true })
    // não bloqueia, assume que existe se where não falhar rápido (fallback)
  } catch {}
  return 'cloudflared'
}

function stopGrok(): void {
  if (!grokProcess || !grokProcess.pid) {
    grokProcess = null
    return
  }
  try {
    if (process.platform === 'win32') {
      spawn('taskkill', ['/pid', String(grokProcess.pid), '/T', '/F'], { windowsHide: true, stdio: 'ignore' })
    } else {
      grokProcess.kill()
    }
  } catch {}
  grokProcess = null
  grokUrl = null
  grokStatus = 'off'
}

function startGrok(): void {
  if (grokProcess) return
  const bin = findCloudflared() || 'cloudflared'
  grokStatus = 'starting'
  grokError = null
  grokUrl = null
  try {
    // tenta quick tunnel sem conta: cloudflared tunnel --url http://127.0.0.1:8734 --no-autoupdate
    grokProcess = spawn(bin, ['tunnel', '--url', 'http://127.0.0.1:8734', '--no-autoupdate'], {
      windowsHide: true,
      stdio: ['ignore', 'pipe', 'pipe'],
    })
  } catch (e) {
    grokStatus = 'error'
    grokError = e instanceof Error ? e.message : String(e)
    grokProcess = null
    return
  }
  let buffer = ''
  const onData = (chunk: Buffer) => {
    buffer += chunk.toString()
    // cloudflared imprime URL em stderr/stdout como https://xxx.trycloudflare.com
    const m = buffer.match(/https:\/\/[a-z0-9-]+\.trycloudflare\.com/gi)
    if (m && m[0] && !grokUrl) {
      grokUrl = m[0]
      grokStatus = 'online'
      // notifica janelas
      for (const w of BrowserWindow.getAllWindows()) {
        w.webContents.send('grok:url', grokUrl)
      }
    }
  }
  grokProcess!.stdout?.on('data', onData)
  grokProcess!.stderr?.on('data', onData)
  grokProcess!.on('exit', (code) => {
    if (grokStatus !== 'online') {
      grokStatus = 'error'
      grokError = `cloudflared saiu com código ${code}`
    } else if (code !== 0 && code !== null) {
      grokStatus = 'error'
      grokError = `tunnel caiu (código ${code})`
    }
    grokProcess = null
  })
  grokProcess!.on('error', (err) => {
    grokStatus = 'error'
    grokError = err.message
    grokProcess = null
  })
  // fallback: se não capturar em 12s, marca erro mas mantém processo
  setTimeout(() => {
    if (grokStatus === 'starting' && !grokUrl) {
      // ainda sem URL, pode ser que cloudflared precise de mais tempo ou não está instalado
      // não marca erro ainda, deixa tentar
    }
  }, 12000)
}

async function healthOk(): Promise<boolean> {
  for (const url of [`${API_BASE}/api/health`, `${API_BASE}/health`]) {
    try {
      const res = await fetch(url)
      if (res.ok) return true
    } catch {
      // backend ainda subindo
    }
  }
  return false
}

async function waitForBackend(timeoutMs: number): Promise<void> {
  const deadline = Date.now() + timeoutMs
  while (Date.now() < deadline) {
    if (await healthOk()) return
    await new Promise((r) => setTimeout(r, 300))
  }
}

async function createWindow(): Promise<void> {
  const win = new BrowserWindow({
    width: 1280,
    height: 840,
    minWidth: 640,
    minHeight: 480,
    title: 'Artigos Ana',
    frame: false,
    icon: path.join(__dirname, '..', 'assets', 'artigos-ana.ico'),
    backgroundColor: '#fafaf9',
    show: false,
    webPreferences: {
      preload: path.join(__dirname, 'preload.js'),
      contextIsolation: true,
      nodeIntegration: false,
    },
  })
  win.once('ready-to-show', () => win.show())

  const devUrl = process.env.VITE_DEV_SERVER_URL
  if (devUrl) {
    await win.loadURL(devUrl)
  } else {
    await win.loadFile(path.join(__dirname, '..', 'dist', 'index.html'))
  }
}

ipcMain.handle('abrir-pasta', (_event, caminho: string) => {
  shell.showItemInFolder(caminho)
})

ipcMain.handle('abrir-arquivo', (_event, caminho: string) => {
  return shell.openPath(caminho)
})

ipcMain.handle('abrir-externo', (_event, url: string) => {
  let parsed: URL
  try {
    parsed = new URL(url)
  } catch {
    throw new Error('URL invalida')
  }
  if (parsed.protocol !== 'http:' && parsed.protocol !== 'https:') {
    throw new Error('URL deve usar http ou https')
  }
  return shell.openExternal(url)
})

ipcMain.handle('janela-minimizar', (event) => {
  BrowserWindow.fromWebContents(event.sender)?.minimize()
})

ipcMain.handle('janela-alternar-maximizar', (event) => {
  const janela = BrowserWindow.fromWebContents(event.sender)
  if (!janela) return
  if (janela.isMaximized()) janela.unmaximize()
  else janela.maximize()
})

ipcMain.handle('janela-fechar', (event) => {
  BrowserWindow.fromWebContents(event.sender)?.close()
})

ipcMain.handle('grok:get', () => {
  return { url: grokUrl, status: grokStatus, error: grokError }
})

ipcMain.handle('grok:restart', async () => {
  stopGrok()
  await new Promise((r) => setTimeout(r, 800))
  startGrok()
  // aguarda até 10s por URL
  const deadline = Date.now() + 10000
  while (Date.now() < deadline) {
    if (grokUrl) return { url: grokUrl, status: grokStatus, error: grokError }
    await new Promise((r) => setTimeout(r, 400))
  }
  return { url: grokUrl, status: grokStatus, error: grokError }
})

ipcMain.handle('grok:copy', async (_event, url: string) => {
  const { clipboard } = await import('electron')
  clipboard.writeText(url)
})

app.setAppUserModelId('artigos.ana')

app.whenReady().then(async () => {
  startBackend()
  await waitForBackend(15000)
  // habilita GROK automaticamente para acesso web cross-network (mesmo em rede diferente)
  // funciona via Cloudflare edge, não via LAN direta
  startGrok()
  await createWindow()

  app.on('activate', () => {
    if (BrowserWindow.getAllWindows().length === 0) {
      void createWindow()
    }
  })
})

app.on('window-all-closed', () => {
  if (process.platform !== 'darwin') app.quit()
})

app.on('before-quit', () => {
  stopGrok()
  stopBackend()
})
app.on('will-quit', () => {
  stopGrok()
  stopBackend()
})
