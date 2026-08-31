export function isIpadUA(ua: string, maxTouchPoints: number, hasTouch: boolean): boolean {
  const normalized = ua || ""
  // iPad clássico
  if (/iPad/.test(normalized)) return true
  // iPadOS 13+ se passa por Macintosh mas tem toque e mais de 1 ponto
  if (/Macintosh/.test(normalized) && hasTouch && maxTouchPoints > 1) return true
  return false
}

export function detectIpad(): boolean {
  if (typeof navigator === "undefined" || typeof window === "undefined") return false
  const ua = navigator.userAgent || ""
  const maxTouch = (navigator as unknown as { maxTouchPoints?: number }).maxTouchPoints ?? 0
  const hasTouch = "ontouchend" in window || maxTouch > 0
  return isIpadUA(ua, maxTouch, hasTouch)
}

export function isIpadWeb(): boolean {
  if (typeof window === "undefined") return false
  // só web: se for Electron (window.artigosAna existe), não aplica nativo iPad web
  if (window.artigosAna) return false
  return detectIpad()
}
