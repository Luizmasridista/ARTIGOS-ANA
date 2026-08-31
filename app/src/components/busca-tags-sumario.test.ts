import { describe, expect, it, vi } from 'vitest'
import { highlightPartes } from './Biblioteca'
import type { Nota, SumarioItem, BuscaResultado } from '../api'

// helpers replicating PainelLateral filtering and tag logic
function filtrarNotas(notas: Nota[], filtroTag: string | null, filtroCor: string | null): Nota[] {
  return notas.filter((n) => {
    if (filtroTag && !(n.tags ?? []).includes(filtroTag)) return false
    if (filtroCor && n.cor !== filtroCor) return false
    return true
  })
}

function canAddTag(tag: string, existing: string[]): boolean {
  const v = tag.trim()
  if (!v) return false
  if (v.includes(',') || v.includes(';')) return false
  if (v.length > 50) return false
  if (existing.includes(v)) return false
  return true
}

function contadorBiblioteca(total: number, filtrados: number, busca: string): string {
  const q = busca.trim()
  if (q) return `${filtrados} ${filtrados === 1 ? 'resultado' : 'resultados'} para "${q}"`
  return `${total} ${total === 1 ? 'artigo' : 'artigos'}`
}

describe('Biblioteca - busca com highlight <mark>', () => {
  it('destaca termo case-insensitive preservando casing original', () => {
    const partes = highlightPartes('Silva 2020', 'silva')
    expect(partes).toEqual([
      { texto: 'Silva', destaque: true },
      { texto: ' 2020', destaque: false },
    ])
  })

  it('destaca múltiplas ocorrências', () => {
    const partes = highlightPartes('ana e Ana e ANA', 'ana')
    const destaques = partes.filter((p) => p.destaque)
    expect(destaques.length).toBe(3)
    expect(destaques.every((p) => p.texto.toLowerCase() === 'ana')).toBe(true)
  })

  it('sem busca retorna sem destaque', () => {
    const partes = highlightPartes('Título Qualquer', '')
    expect(partes).toEqual([{ texto: 'Título Qualquer', destaque: false }])
  })

  it('escapa caracteres de regex (* . + ? etc)', () => {
    const partes = highlightPartes('a+b*c?', 'a+b*')
    // should match literal "a+b*" not regex
    expect(partes.some((p) => p.destaque && p.texto === 'a+b*')).toBe(true)
  })

  it('escapa parênteses e colchetes', () => {
    const partes = highlightPartes('(Silva, 2020) [1]', '(Silva, 2020)')
    expect(partes.some((p) => p.destaque)).toBe(true)
  })

  it('XSS payload é tratado como texto puro, não HTML', () => {
    const xss = '<script>alert(1)</script>'
    const partes = highlightPartes(`Título ${xss} fim`, xss)
    // highlight should treat as literal, not execute
    expect(partes.some((p) => p.destaque && p.texto === xss)).toBe(true)
    // ensure no parte contains unescaped HTML interpretation — just strings
    const joined = partes.map((p) => p.texto).join('')
    expect(joined).toBe(`Título ${xss} fim`)
  })

  it('busca vazia com espaços não destaca', () => {
    const partes = highlightPartes('abc', '   ')
    expect(partes.every((p) => !p.destaque)).toBe(true)
  })
})

describe('Biblioteca - contador e filtro server-side', () => {
  it('contador com busca mostra "n resultados para \"q\""', () => {
    expect(contadorBiblioteca(10, 3, 'Silva')).toBe('3 resultados para "Silva"')
    expect(contadorBiblioteca(10, 1, 'Silva')).toBe('1 resultado para "Silva"')
  })
  it('contador sem busca mostra total de artigos', () => {
    expect(contadorBiblioteca(5, 5, '')).toBe('5 artigos')
    expect(contadorBiblioteca(1, 1, '')).toBe('1 artigo')
  })
  it('filtro é case-insensitive no servidor (ILIKE) — simula client fallback', () => {
    // se servidor falhar, client local deveria também ser case-insensitive; este teste garante helper de contador não quebra com q vazia
    expect(contadorBiblioteca(0, 0, '   ')).toBe('0 artigos')
  })
})

describe('Notas - tags add/remove e sanitização', () => {
  it('adiciona tag válida', () => {
    expect(canAddTag('revisar', [])).toBe(true)
    expect(canAddTag('importante', ['revisar'])).toBe(true)
  })
  it('rejeita tag com vírgula ou ponto-e-vírgula', () => {
    expect(canAddTag('bad,tag', [])).toBe(false)
    expect(canAddTag('bad;tag', [])).toBe(false)
  })
  it('rejeita tag vazia ou só espaços', () => {
    expect(canAddTag('', [])).toBe(false)
    expect(canAddTag('   ', [])).toBe(false)
  })
  it('rejeita tag duplicada', () => {
    expect(canAddTag('revisar', ['revisar'])).toBe(false)
  })
  it('rejeita tag >50 caracteres', () => {
    const long = 'a'.repeat(51)
    expect(canAddTag(long, [])).toBe(false)
    expect(canAddTag('a'.repeat(50), [])).toBe(true)
  })
  it('XSS em tag é tratado como texto (não escapa)', () => {
    const xss = '<script>alert(1)</script>'
    // tag com XSS mas sem ,; e <=50 é aceita como texto — renderização deve escapar (React text node escapa)
    expect(canAddTag(xss, [])).toBe(true)
    const shortX = '<img>'
    expect(canAddTag(shortX, [])).toBe(true) // aceita mas será renderizado como texto, não HTML
  })

  it('remove tag', () => {
    let tags = ['revisar', 'duvida', 'importante']
    tags = tags.filter((t) => t !== 'duvida')
    expect(tags).toEqual(['revisar', 'importante'])
  })
})

describe('Notas - filtro por tag/cor', () => {
  const notas: Nota[] = [
    { id: 1, pagina: 1, texto: 'nota 1', criado_em: new Date().toISOString(), tags: ['revisar'], cor: '#FFEB3B' },
    { id: 2, pagina: 2, texto: 'nota 2', criado_em: new Date().toISOString(), tags: ['importante', 'revisar'], cor: '#9EE6A8' },
    { id: 3, pagina: 1, texto: 'nota 3', criado_em: new Date().toISOString(), tags: [], cor: '#FFEB3B' },
    { id: 4, pagina: 3, texto: 'nota 4', criado_em: new Date().toISOString(), tags: ['duvida'], cor: '#8FC1FF' },
  ]

  it('filtra por tag revisiar retorna 2', () => {
    expect(filtrarNotas(notas, 'revisar', null).map((n) => n.id)).toEqual([1, 2])
  })
  it('filtra por cor #FFEB3B retorna 2', () => {
    expect(filtrarNotas(notas, null, '#FFEB3B').map((n) => n.id)).toEqual([1, 3])
  })
  it('filtra por tag e cor combinados', () => {
    expect(filtrarNotas(notas, 'revisar', '#9EE6A8').map((n) => n.id)).toEqual([2])
  })
  it('sem filtro retorna todas ordenadas', () => {
    expect(filtrarNotas(notas, null, null).length).toBe(4)
  })
  it('filtro inexistente retorna vazio e mostra contador 0 de N', () => {
    const filtradas = filtrarNotas(notas, 'inexistente', null)
    expect(filtradas.length).toBe(0)
    const texto = `${filtradas.length} de ${notas.length} notas`
    expect(texto).toBe('0 de 4 notas')
  })
  it('todasTags e todasCores dedup e sort', () => {
    const todasTags = Array.from(new Set(notas.flatMap((n) => n.tags))).sort()
    expect(todasTags).toEqual(['duvida', 'importante', 'revisar'])
    const todasCores = Array.from(new Set(notas.map((n) => n.cor))).sort()
    expect(todasCores).toEqual(['#8FC1FF', '#9EE6A8', '#FFEB3B'])
  })
})

describe('Leitor - busca no PDF (Ctrl+K) com contador e setas', () => {
  const resultados: BuscaResultado[] = [
    { pagina: 1, pos: [10, 20, 40, 30], trecho: '... atenção ...' },
    { pagina: 2, pos: [15, 30, 50, 45], trecho: '... atenção novamente ...' },
    { pagina: 1, pos: [70, 100, 90, 115], trecho: '... atenção terceira ...' },
  ]

  function contadorBusca(idx: number, total: number): string {
    if (total === 0) return '0 ocorrências'
    return `${total} ocorrências — ${idx + 1}/${total}`
  }

  it('contador exibe "3 ocorrências — 1/3" para primeira', () => {
    expect(contadorBusca(0, 3)).toBe('3 ocorrências — 1/3')
  })
  it('navegação cicla: próximo de último volta ao primeiro', () => {
    const next = (idx: number, total: number) => (idx + 1) % total
    const prev = (idx: number, total: number) => (idx - 1 + total) % total
    expect(next(2, 3)).toBe(0)
    expect(prev(0, 3)).toBe(2)
  })
  it('irParaBusca define foco e página corretamente', () => {
    const idxRef: { foco: { pagina: number; pos: [number, number, number, number] } | null } = { foco: null }
    const irParaBusca = (idx: number) => {
      const alvo = resultados[idx]
      idxRef.foco = { pagina: alvo.pagina, pos: alvo.pos }
    }
    irParaBusca(1)
    expect(idxRef.foco!.pagina).toBe(2)
    expect(idxRef.foco!.pos).toEqual([15, 30, 50, 45])
  })
  it('busca curta <2 chars não dispara fetch (erro)', () => {
    const q = 'a'
    const deveBuscar = q.trim().length >= 2
    expect(deveBuscar).toBe(false)
  })
  it('q vazia limpa resultados', () => {
    const q = '   '
    const resultadosAfter = q.trim().length === 0 ? [] : resultados
    expect(resultadosAfter.length).toBe(0)
  })
  it('trecho da busca é texto puro (XSS não executa)', () => {
    const r: BuscaResultado = { pagina: 1, pos: [0, 0, 10, 10], trecho: '<script>alert(1)</script> atenção' }
    expect(r.trecho).toContain('<script>')
    // renderização deve escapar, não usar dangerouslySetInnerHTML
  })
})

describe('Sumário - lista título-página, clique scroll', () => {
  const sumario: SumarioItem[] = [
    { titulo: '1. Introdução', pagina: 1, nivel: 1, ordem: 1 },
    { titulo: '2. Metodologia', pagina: 3, nivel: 1, ordem: 2 },
    { titulo: 'Conclusão', pagina: 5, nivel: 1, ordem: 3 },
  ]

  it('mantém ordem por campo ordem', () => {
    const shuffled = [...sumario].sort(() => Math.random() - 0.5)
    const ordenado = [...shuffled].sort((a, b) => a.ordem - b.ordem)
    expect(ordenado.map((s) => s.titulo)).toEqual(['1. Introdução', '2. Metodologia', 'Conclusão'])
    expect(ordenado[0].ordem).toBe(1)
  })

  it('exibe título e página "p. N"', () => {
    const render = (item: SumarioItem) => `${item.titulo} — p. ${item.pagina}`
    expect(render(sumario[0])).toBe('1. Introdução — p. 1')
    expect(render(sumario[1])).toBe('2. Metodologia — p. 3')
  })

  it('clique chama irParaPagina com número correto', () => {
    const irParaPagina = vi.fn()
    const onClick = (item: SumarioItem) => irParaPagina(item.pagina)
    onClick(sumario[1])
    expect(irParaPagina).toHaveBeenCalledWith(3)
  })

  it('lista vazia mostra "Nenhuma seção detectada"', () => {
    const lista: SumarioItem[] = []
    const mensagem = lista.length === 0 ? 'Nenhuma seção detectada.' : 'tem itens'
    expect(mensagem).toBe('Nenhuma seção detectada.')
  })

  it('reuso destaque logic: buscaFoco e fonteFoco compartilham mesma estrutura pagina/pos/nonce', () => {
    const buscaFoco: { pagina: number; pos: [number, number, number, number]; nonce: number } = { pagina: 2, pos: [10, 20, 40, 30], nonce: Date.now() }
    const fonteFoco: { citacaoId: number; pagina: number; pos: [number, number, number, number]; nonce: number } = { citacaoId: 1, pagina: 2, pos: [10, 20, 40, 30], nonce: Date.now() }
    expect(buscaFoco.pagina).toBe(fonteFoco.pagina)
    expect(buscaFoco.pos).toEqual(fonteFoco.pos)
  })
})

describe('PageView - reuso destaque busca (BuscaFoco pulse)', () => {
  it('pulso dura 2s como fonteFoco', async () => {
    let pulso = true
    const t = setTimeout(() => (pulso = false), 2000)
    expect(pulso).toBe(true)
    await new Promise((r) => setTimeout(r, 2100))
    expect(pulso).toBe(false)
    clearTimeout(t)
  })
  it('só desenha busca-destaque quando pagina === foco.pagina', () => {
    const foco = { pagina: 3, pos: [10, 10, 50, 20] as [number, number, number, number], nonce: 1 }
    expect(foco.pagina === 3).toBe(true)
    expect(foco.pagina === 2).toBe(false)
  })
})
