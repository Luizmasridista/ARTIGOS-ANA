import type { Palavra, PalavraBox } from '../api'

export function indicePalavraProxima(palavras: Palavra[], x: number, y: number, margem = 50): number | null {
  let melhor = -1
  let menorDist = Infinity
  for (let i = 0; i < palavras.length; i++) {
    const p = palavras[i]
    const dx = Math.max(p.x0 - x, 0, x - p.x1)
    const dy = Math.max(p.y0 - y, 0, y - p.y1)
    const dist = dx * dx + dy * dy
    if (dist < menorDist) {
      menorDist = dist
      melhor = i
    }
  }
  if (melhor < 0 || menorDist > margem * margem) return null
  return melhor
}

function segmentoIntersectaCaixa(
  ax: number,
  ay: number,
  bx: number,
  by: number,
  x0: number,
  y0: number,
  x1: number,
  y1: number,
  tol: number,
): boolean {
  const dx = bx - ax
  const dy = by - ay
  let t0 = 0
  let t1 = 1
  if (Math.abs(dx) < 1e-9) {
    if (ax < x0 - tol || ax > x1 + tol) return false
  } else {
    let ta = (x0 - tol - ax) / dx
    let tb = (x1 + tol - ax) / dx
    if (ta > tb) [ta, tb] = [tb, ta]
    t0 = Math.max(t0, ta)
    t1 = Math.min(t1, tb)
  }
  if (t0 > t1) return false
  if (Math.abs(dy) < 1e-9) {
    if (ay < y0 - tol || ay > y1 + tol) return false
  } else {
    let ta = (y0 - tol - ay) / dy
    let tb = (y1 + tol - ay) / dy
    if (ta > tb) [ta, tb] = [tb, ta]
    t0 = Math.max(t0, ta)
    t1 = Math.min(t1, tb)
  }
  return t0 <= t1
}

export function palavrasNoSegmento(
  palavras: Palavra[],
  ax: number,
  ay: number,
  bx: number,
  by: number,
  tolerancia = 2,
): number[] {
  const indices: number[] = []
  for (let i = 0; i < palavras.length; i++) {
    const p = palavras[i]
    if (segmentoIntersectaCaixa(ax, ay, bx, by, p.x0, p.y0, p.x1, p.y1, tolerancia)) {
      indices.push(i)
    }
  }
  return indices
}

export function boxesDasPalavras(palavras: Palavra[]): PalavraBox[] {
  return palavras.map((p) => [p.x0, p.y0, p.x1, p.y1])
}

export function posicaoDePalavra(p: Palavra, scale: number) {
  return {
    left: p.x0 * scale,
    top: p.y0 * scale,
    width: (p.x1 - p.x0) * scale,
    height: (p.y1 - p.y0) * scale,
    fontSize: (p.y1 - p.y0) * scale,
  }
}

export function agruparEmSegmentos(boxes: PalavraBox[]): PalavraBox[] {
  const ordenadas = [...boxes].sort((a, b) => (a[1] === b[1] ? a[0] - b[0] : a[1] - b[1]))
  const segmentos: PalavraBox[] = []
  for (const box of ordenadas) {
    const ultimo = segmentos[segmentos.length - 1]
    if (ultimo) {
      const altura = box[3] - box[1]
      const centro = (box[1] + box[3]) / 2
      const centroUltimo = (ultimo[1] + ultimo[3]) / 2
      const mesmaLinha = Math.abs(centro - centroUltimo) < altura * 0.6
      const adjacente = box[0] - ultimo[2] <= altura * 0.3
      if (mesmaLinha && adjacente) {
        ultimo[2] = Math.max(ultimo[2], box[2])
        continue
      }
    }
    segmentos.push([box[0], box[1], box[2], box[3]])
  }
  return segmentos
}

export function agruparTexto(palavras: Palavra[]): string {
  const linhas: string[][] = []
  let atual: string[] | null = null
  let centroAnterior: number | null = null
  let alturaAnterior = 0
  for (const p of palavras) {
    const centro = (p.y0 + p.y1) / 2
    const altura = p.y1 - p.y0
    if (centroAnterior !== null && Math.abs(centro - centroAnterior) > ((altura + alturaAnterior) / 2) * 0.5) {
      atual = null
    }
    if (!atual) {
      atual = []
      linhas.push(atual)
    }
    atual.push(p.texto)
    centroAnterior = centro
    alturaAnterior = altura
  }
  return linhas.map((l) => l.join(' ')).join('\n')
}
