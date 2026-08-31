import { contextBridge, ipcRenderer } from 'electron'

contextBridge.exposeInMainWorld('artigosAna', {
  abrirPasta: (caminho: string): Promise<void> => ipcRenderer.invoke('abrir-pasta', caminho),
  abrirArquivo: (caminho: string): Promise<string> => ipcRenderer.invoke('abrir-arquivo', caminho),
  abrirExterno: (url: string): Promise<void> => ipcRenderer.invoke('abrir-externo', url),
  janela: {
    minimizar: (): Promise<void> => ipcRenderer.invoke('janela-minimizar'),
    alternarMaximizar: (): Promise<void> => ipcRenderer.invoke('janela-alternar-maximizar'),
    fechar: (): Promise<void> => ipcRenderer.invoke('janela-fechar'),
  },
  grok: {
    get: (): Promise<{ url: string | null; status: string; error: string | null }> => ipcRenderer.invoke('grok:get'),
    restart: (): Promise<{ url: string | null; status: string; error: string | null }> => ipcRenderer.invoke('grok:restart'),
    copiar: (url: string): Promise<void> => ipcRenderer.invoke('grok:copy', url),
    onUrl: (cb: (url: string) => void) => {
      const handler = (_e: unknown, u: string) => cb(u)
      ipcRenderer.on('grok:url', handler)
      return () => ipcRenderer.removeListener('grok:url', handler)
    },
  },
})
