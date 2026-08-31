import { describe, expect, it } from 'vitest'
import type { Palavra } from '../api'
import {
  agruparEmSegmentos,
  agruparTexto,
  boxesDasPalavras,
  indicePalavraProxima,
  palavrasNoSegmento,
  posicaoDePalavra,
} from './geometry'

function palavra(texto: string, x0: number, y0: number, x1: number, y1: number): Palavra {
  return { texto, x0, y0, x1, y1 }
}

const palavras = [
  palavra('a1', 100, 100, 140, 115),
  palavra('a2', 150, 100, 210, 115),
  palavra('a3', 220, 100, 300, 115),
  palavra('b1', 100, 200, 160, 215),
  palavra('b2', 170, 200, 260, 215),
  palavra('c1', 100, 300, 170, 315),
  palavra('c2', 180, 300, 250, 315),
]

describe('posicaoDePalavra (mapeamento camada -> tela)', () => {
  it('posiciona a palavra pela regra de três exata', () => {
    const p = palavra('Olá', 208.33, 182.7, 289.37, 212.95)
    const pos = posicaoDePalavra(p, 0.5)
    expect(pos.left).toBeCloseTo(104.165, 2)
    expect(pos.top).toBeCloseTo(91.35, 2)
    expect(pos.width).toBeCloseTo(40.52, 2)
    expect(pos.height).toBeCloseTo(15.125, 2)
    expect(pos.fontSize).toBeCloseTo(15.125, 2)
  })

  it('com scale 1 os pixels da camada viram pixels de tela 1:1', () => {
    const p = palavra('X', 100, 200, 140, 230)
    const pos = posicaoDePalavra(p, 1)
    expect(pos.left).toBe(100)
    expect(pos.top).toBe(200)
    expect(pos.width).toBe(40)
  })
})

describe('indicePalavraProxima (cursor do mouse -> palavra)', () => {
  it('aponta exatamente para a palavra sob o cursor', () => {
    expect(palavras[indicePalavraProxima(palavras, 120, 107)!].texto).toBe('a1')
    expect(palavras[indicePalavraProxima(palavras, 120, 207)!].texto).toBe('b1')
  })

  it('pouco além do fim da linha pega a última palavra da linha', () => {
    const idx = indicePalavraProxima(palavras, 330, 107)
    expect(palavras[idx!].texto).toBe('a3')
  })

  it('no vão entre linhas pega a palavra mais próxima', () => {
    const idx = indicePalavraProxima(palavras, 120, 140)
    expect(palavras[idx!].texto).toBe('a1')
  })

  it('longe de qualquer palavra devolve null', () => {
    expect(indicePalavraProxima(palavras, 60, 60)).toBeNull()
    expect(indicePalavraProxima(palavras, 500, 500)).toBeNull()
  })
})

describe('palavrasNoSegmento (a tinta segue o caminho do cursor)', () => {
  const nomes = (idx: number[]) => idx.map((i) => palavras[i].texto)

  it('linha horizontal no meio de uma linha pega as palavras atravessadas', () => {
    expect(nomes(palavrasNoSegmento(palavras, 105, 107, 295, 107))).toEqual(['a1', 'a2', 'a3'])
  })

  it('coluna vertical atravessando três linhas pega só a coluna do cursor', () => {
    expect(nomes(palavrasNoSegmento(palavras, 155, 105, 155, 310))).toEqual(['a2', 'b1', 'c1'])
  })

  it('REGRESSÃO: palavras na ordem de leitura entre as pontas, mas fora do traço, NÃO entram', () => {
    const idx = palavrasNoSegmento(palavras, 200, 105, 200, 310)
    expect(nomes(idx)).toEqual(['a2', 'b2', 'c2'])
  })

  it('diagonal atravessando linhas diferentes', () => {
    const idx = palavrasNoSegmento(palavras, 105, 105, 295, 310)
    expect(nomes(idx)).toEqual(['a1', 'b2'])
  })

  it('segmento no vão entre palavras devolve vazio', () => {
    expect(palavrasNoSegmento(palavras, 60, 60, 90, 70)).toEqual([])
  })
})

describe('agruparEmSegmentos (retângulos do destaque)', () => {
  it('junta palavras vizinhas da mesma linha em um segmento', () => {
    const boxes = boxesDasPalavras([palavra('a', 10, 10, 30, 25), palavra('b', 32, 10, 60, 25)])
    expect(agruparEmSegmentos(boxes)).toEqual([[10, 10, 60, 25]])
  })

  it('não junta palavras distantes na mesma linha', () => {
    const boxes = boxesDasPalavras([palavra('a', 10, 10, 30, 25), palavra('b', 300, 10, 340, 25)])
    expect(agruparEmSegmentos(boxes)).toEqual([
      [10, 10, 30, 25],
      [300, 10, 340, 25],
    ])
  })

  it('não junta linhas diferentes', () => {
    const boxes = boxesDasPalavras([palavra('a', 10, 10, 30, 25), palavra('b', 10, 80, 30, 95)])
    expect(agruparEmSegmentos(boxes).length).toBe(2)
  })
})

describe('agruparTexto (texto da marcação)', () => {
  it('quebra linhas corretamente', () => {
    const texto = agruparTexto([palavra('Olá', 10, 10, 30, 25), palavra('mundo', 32, 10, 70, 25), palavra('linha2', 10, 60, 50, 75)])
    expect(texto).toBe('Olá mundo\nlinha2')
  })
})
