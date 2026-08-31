export function registerSW(): void {
  if (typeof window === "undefined") return
  // Nao registrar no Electron
  if (window.artigosAna) return
  if (!("serviceWorker" in navigator)) return

  // VitePWA gera /sw.js na raiz do dist; registrar apos load para nao bloquear
  const onLoad = () => {
    navigator.serviceWorker
      .register("/sw.js")
      .then((reg) => {
        // autoUpdate: SW atualiza sozinho, mas log para debug
        if (reg.waiting) {
          // nova versao esperando
          // autoUpdate fara skipWaiting no SW gerado
        }
        reg.addEventListener("updatefound", () => {
          const sw = reg.installing
          if (!sw) return
          sw.addEventListener("statechange", () => {
            if (sw.state === "installed" && navigator.serviceWorker.controller) {
              // nova versao instalada, pagina sera controlada no reload
            }
          })
        })
      })
      .catch(() => {
        // silencioso: falha de registro nao deve quebrar app web
      })
  }

  if (document.readyState === "complete") {
    onLoad()
  } else {
    window.addEventListener("load", onLoad, { once: true })
  }
}
