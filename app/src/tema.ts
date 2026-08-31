export type Tema = 'light' | 'dark'

export function temaInicial(): Tema {
  const salvo = localStorage.getItem('artigos-ana-tema')
  if (salvo === 'light' || salvo === 'dark') return salvo
  return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

export function aplicarTema(tema: Tema): void {
  document.documentElement.dataset.theme = tema
  localStorage.setItem('artigos-ana-tema', tema)
}
