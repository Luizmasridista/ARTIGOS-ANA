export {}

declare global {
  interface Window {
    artigosAna?: {
      abrirPasta(caminho: string): Promise<void>
      abrirArquivo(caminho: string): Promise<string>
      abrirExterno(url: string): Promise<void>
      janela: {
        minimizar(): Promise<void>
        alternarMaximizar(): Promise<void>
        fechar(): Promise<void>
      }
      grok?: {
        get(): Promise<{ url: string | null; status: string; error: string | null }>
        restart(): Promise<{ url: string | null; status: string; error: string | null }>
        copiar(url: string): Promise<void>
        onUrl(cb: (url: string) => void): () => void
      }
    }
  }
}
