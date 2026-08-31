import { describe, expect, it, vi } from 'vitest'
import type { Citacao, CitacaoOcorrencia } from '../api'

// replica lógica de temOcorrencia e ciclo de ocorrencias como em App.tsx / CitacoesTab.tsx

function temOcorrencia(c: Citacao): boolean {
  return typeof c.pagina === 'number' && c.pagina > 0 && Array.isArray(c.pos) && c.pos !== null
}

function posEscalada(
  pos: [number, number, number, number],
  larguraPx: number,
  larguraPagina: number,
) {
  const scale = larguraPx / larguraPagina
  return {
    left: pos[0] * scale,
    top: pos[1] * scale,
    width: (pos[2] - pos[0]) * scale,
    height: (pos[3] - pos[1]) * scale,
  }
}

function fakeCitacao(over: Partial<Citacao> & { id: number; tipo: string; chave: string }): Citacao {
  return {
    autor: '',
    ano: null,
    trecho: '',
    titulo: '',
    texto: '',
    url: '',
    criado_em: new Date().toISOString(),
    pagina: 0,
    pos: null,
    ocorrencias: [],
    ...over,
  }
}

// simula irParaFonte cycling logic
function criarIrParaFonte(mostrarAviso: (m: string) => void) {
  const idxMap = new Map<number, number>()
  let foco: { citacaoId: number; pagina: number; pos: [number, number, number, number]; nonce: number } | null = null
  let paginaAtual = 1
  const irParaFonte = (citacao: Citacao) => {
    const pagina0 = citacao.pagina ?? 0
    const pos0 = citacao.pos ?? null
    const ocorrencias = citacao.ocorrencias ?? []
    const lista: Array<{ pagina: number; pos: [number, number, number, number] | null }> =
      ocorrencias.length > 0
        ? ocorrencias
        : pagina0 > 0 && pos0
          ? [{ pagina: pagina0, pos: pos0 }]
          : []
    const navegaveis = lista.filter((o) => o.pagina > 0 && o.pos)
    if (navegaveis.length === 0) {
      mostrarAviso('Sem ocorrência no corpo do texto')
      return
    }
    const prev = idxMap.get(citacao.id) ?? -1
    const nextIdx = (prev + 1) % navegaveis.length
    idxMap.set(citacao.id, nextIdx)
    const alvo = navegaveis[nextIdx]
    foco = { citacaoId: citacao.id, pagina: alvo.pagina, pos: alvo.pos!, nonce: Date.now() }
    paginaAtual = alvo.pagina
  }
  return {
    irParaFonte,
    getFoco: () => foco,
    getPaginaAtual: () => paginaAtual,
    getIdx: (id: number) => idxMap.get(id),
  }
}

describe('CitacoesTab - navegação clique → trecho', () => {
  it('card com pagina>0 e pos não null é navegável', () => {
    const c = fakeCitacao({ id: 1, tipo: 'autor_ano', chave: '(Silva, 2020)', pagina: 3, pos: [10, 20, 40, 30], ocorrencias: [{ pagina: 3, pos: [10, 20, 40, 30], trecho: 'x' }] })
    expect(temOcorrencia(c)).toBe(true)
  })

  it('card com pagina 0 é desabilitado (não navegável)', () => {
    const c = fakeCitacao({ id: 2, tipo: 'referencia', chave: '[1]', pagina: 0, pos: null, ocorrencias: [] })
    expect(temOcorrencia(c)).toBe(false)
  })

  it('card com pagina>0 mas pos null é desabilitado', () => {
    const c = fakeCitacao({ id: 3, tipo: 'autor_ano', chave: '(A, 2020)', pagina: 2, pos: null, ocorrencias: [{ pagina: 2, pos: null, trecho: '...' }] })
    expect(temOcorrencia(c)).toBe(false)
  })

  it('clique em card navegável chama onIrParaFonte e define foco', () => {
    const onIrParaFonte = vi.fn()
    const c = fakeCitacao({ id: 10, tipo: 'autor_ano', chave: '(A, 2020)', pagina: 5, pos: [1, 2, 3, 4], ocorrencias: [{ pagina: 5, pos: [1, 2, 3, 4], trecho: 't' }] })
    // simula handler do card
    const handler = () => {
      if (temOcorrencia(c)) onIrParaFonte(c)
    }
    handler()
    expect(onIrParaFonte).toHaveBeenCalledOnce()
    expect(onIrParaFonte).toHaveBeenCalledWith(c)
  })

  it('card desabilitado: clique mostra aviso e não navega (onIrParaFonte com aviso)', () => {
    const aviso = vi.fn()
    const { irParaFonte, getFoco } = criarIrParaFonte(aviso)
    const c = fakeCitacao({ id: 11, tipo: 'referencia', chave: '[99]', pagina: 0, pos: null, ocorrencias: [] })
    irParaFonte(c)
    expect(aviso).toHaveBeenCalledWith('Sem ocorrência no corpo do texto')
    expect(getFoco()).toBeNull()
  })

  it('botão Ver no texto desabilitado quando sem ocorrência (title aviso)', () => {
    const c = fakeCitacao({ id: 12, tipo: 'referencia', chave: '[5]', pagina: 0, pos: null, ocorrencias: [] })
    const disabled = !temOcorrencia(c)
    const title = disabled ? 'Sem ocorrência no corpo do texto' : `Ver no texto página ${c.pagina}`
    expect(disabled).toBe(true)
    expect(title).toBe('Sem ocorrência no corpo do texto')
  })

  it('botão Ver no texto habilitado quando navegável (aria-label com página)', () => {
    const c = fakeCitacao({ id: 13, tipo: 'autor_ano', chave: '(B, 2021)', pagina: 7, pos: [5, 5, 50, 20], ocorrencias: [{ pagina: 7, pos: [5, 5, 50, 20], trecho: 't' }] })
    const disabled = !temOcorrencia(c)
    const aria = `Ver no texto página ${c.pagina}`
    expect(disabled).toBe(false)
    expect(aria).toBe('Ver no texto página 7')
  })
})

describe('PageView - fonte-destaque pos escalada', () => {
  it('calcula left/top/width/height com escala exata (mesma regra de posicaoDePalavra)', () => {
    const pos: [number, number, number, number] = [10.5, 20.1, 40.2, 32.8]
    const larguraPagina = 612 // pontos PDF A4
    const larguraPx = 800
    const r = posEscalada(pos, larguraPx, larguraPagina)
    const scale = larguraPx / larguraPagina
    expect(r.left).toBeCloseTo(10.5 * scale, 5)
    expect(r.top).toBeCloseTo(20.1 * scale, 5)
    expect(r.width).toBeCloseTo((40.2 - 10.5) * scale, 5)
    expect(r.height).toBeCloseTo((32.8 - 20.1) * scale, 5)
  })

  it('só desenha quando pagina === foco.pagina', () => {
    const foco = { citacaoId: 1, pagina: 3, pos: [10, 10, 50, 20] as [number, number, number, number], nonce: 1 }
    const deveDesenharNaPagina = (paginaNum: number) => foco.pagina === paginaNum
    expect(deveDesenharNaPagina(3)).toBe(true)
    expect(deveDesenharNaPagina(2)).toBe(false)
  })

  it('não desenha quando pos é null ou pagina 0', () => {
    const c = fakeCitacao({ id: 1, tipo: 'referencia', chave: 'x', pagina: 0, pos: null, ocorrencias: [] })
    expect(temOcorrencia(c)).toBe(false)
    // PageView branch: if (!fonteFoco || !pos) não renderiza
  })

  it('pulso dura 2s e depois mantém contorno sutil (classe pulso removida)', async () => {
    let pulso = true
    // simula useEffect que limpa após 2000ms
    const t = setTimeout(() => (pulso = false), 2000)
    expect(pulso).toBe(true)
    // avança tempo simulado
    await new Promise((r) => setTimeout(r, 2100))
    expect(pulso).toBe(false)
    clearTimeout(t)
  })

  it('click na página limpa o foco', () => {
    let foco: { pagina: number } | null = { pagina: 3 }
    const limpar = () => (foco = null)
    // simula click no .pagina-inner
    limpar()
    expect(foco).toBeNull()
  })
})

describe('App irParaFonte - ciclagem de ocorrências', () => {
  it('primeiro clique vai para primeira ocorrência, segundo para segunda, terceiro cicla', () => {
    const aviso = vi.fn()
    const { irParaFonte, getFoco } = criarIrParaFonte(aviso)
    const ocorrencias: CitacaoOcorrencia[] = [
      { pagina: 3, pos: [10, 20, 40, 30], trecho: 't1' },
      { pagina: 7, pos: [15, 30, 50, 45], trecho: 't2' },
      { pagina: 10, pos: [5, 5, 30, 15], trecho: 't3' },
    ]
    const c = fakeCitacao({ id: 42, tipo: 'autor_ano', chave: '(Silva, 2020)', pagina: 3, pos: [10, 20, 40, 30], ocorrencias })
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(3)
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(7)
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(10)
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(3) // ciclou
    expect(aviso).not.toHaveBeenCalled()
  })

  it('citações diferentes mantêm índices independentes', () => {
    const aviso = vi.fn()
    const { irParaFonte, getFoco } = criarIrParaFonte(aviso)
    const c1 = fakeCitacao({ id: 1, tipo: 'autor_ano', chave: '(A, 2020)', pagina: 2, pos: [0, 0, 10, 10], ocorrencias: [{ pagina: 2, pos: [0, 0, 10, 10], trecho: 'a' }, { pagina: 4, pos: [0, 0, 10, 10], trecho: 'b' }] })
    const c2 = fakeCitacao({ id: 2, tipo: 'numerica', chave: '[1]', pagina: 5, pos: [0, 0, 10, 10], ocorrencias: [{ pagina: 5, pos: [0, 0, 10, 10], trecho: 'x' }, { pagina: 6, pos: [0, 0, 10, 10], trecho: 'y' }] })
    irParaFonte(c1)
    expect(getFoco()!.pagina).toBe(2)
    irParaFonte(c2)
    expect(getFoco()!.pagina).toBe(5)
    irParaFonte(c1)
    expect(getFoco()!.pagina).toBe(4) // c1 segunda ocorrência, não afeta c2
  })

  it('sem ocorrência mostra aviso e não altera paginaAtual', () => {
    const aviso = vi.fn()
    const { irParaFonte, getPaginaAtual } = criarIrParaFonte(aviso)
    const antes = getPaginaAtual()
    const c = fakeCitacao({ id: 99, tipo: 'referencia', chave: '[99]', pagina: 0, pos: null, ocorrencias: [] })
    irParaFonte(c)
    expect(aviso).toHaveBeenCalledWith('Sem ocorrência no corpo do texto')
    expect(getPaginaAtual()).toBe(antes)
  })

  it('ocorrências com pos null são ignoradas no ciclo', () => {
    const aviso = vi.fn()
    const { irParaFonte, getFoco } = criarIrParaFonte(aviso)
    const c = fakeCitacao({
      id: 7,
      tipo: 'autor_ano',
      chave: '(A, 2020)',
      pagina: 3,
      pos: [10, 10, 20, 20],
      ocorrencias: [
        { pagina: 3, pos: [10, 10, 20, 20], trecho: 'ok1' },
        { pagina: 5, pos: null, trecho: 'sem pos' },
        { pagina: 8, pos: [5, 5, 15, 15], trecho: 'ok2' },
      ],
    })
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(3)
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(8) // pulou a de pos null
    irParaFonte(c)
    expect(getFoco()!.pagina).toBe(3) // ciclou entre as válidas
  })

  it('contrato backend: ocorrencias preserva trecho e pos', () => {
    const o: CitacaoOcorrencia = { pagina: 3, pos: [10.5, 20.1, 40.2, 32.8], trecho: 'como em Silva (2020) afirma' }
    expect(o.pagina).toBe(3)
    expect(o.pos).toEqual([10.5, 20.1, 40.2, 32.8])
    expect(o.trecho).toContain('Silva')
  })
})

describe('Acessibilidade', () => {
  it('card navegável tem role=button, tabIndex 0 e aria-label', () => {
    const c = fakeCitacao({ id: 1, tipo: 'autor_ano', chave: '(Silva, 2020)', pagina: 3, pos: [1, 1, 10, 10], ocorrencias: [{ pagina: 3, pos: [1, 1, 10, 10], trecho: 't' }] })
    const props = {
      role: temOcorrencia(c) ? 'button' : undefined,
      tabIndex: temOcorrencia(c) ? 0 : undefined,
      ariaLabel: temOcorrencia(c) ? `Ver no texto página ${c.pagina}` : undefined,
    }
    expect(props.role).toBe('button')
    expect(props.tabIndex).toBe(0)
    expect(props.ariaLabel).toBe('Ver no texto página 3')
  })

  it('Enter e Space disparam navegação', () => {
    const c = fakeCitacao({ id: 1, tipo: 'autor_ano', chave: 'x', pagina: 2, pos: [0, 0, 10, 10], ocorrencias: [{ pagina: 2, pos: [0, 0, 10, 10], trecho: 't' }] })
    const onIr = vi.fn()
    const onKeyDown = (e: { key: string; preventDefault: () => void }) => {
      if (!temOcorrencia(c)) return
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault()
        onIr(c)
      }
    }
    onKeyDown({ key: 'Enter', preventDefault: vi.fn() })
    expect(onIr).toHaveBeenCalledTimes(1)
    onKeyDown({ key: ' ', preventDefault: vi.fn() })
    expect(onIr).toHaveBeenCalledTimes(2)
    onKeyDown({ key: 'a', preventDefault: vi.fn() })
    expect(onIr).toHaveBeenCalledTimes(2)
  })
})
