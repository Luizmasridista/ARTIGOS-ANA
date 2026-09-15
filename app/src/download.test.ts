import { afterEach, describe, expect, it, vi } from 'vitest'
import { baixarBlob, nomeArquivoParaDownload } from './download'

describe('nomeArquivoParaDownload', () => {
  afterEach(() => {
    vi.useRealTimers()
    vi.unstubAllGlobals()
  })

  it('mantém o nome fornecido pelo servidor', () => {
    expect(nomeArquivoParaDownload('revisao-final.docx', 'application/vnd.openxmlformats-officedocument.wordprocessingml.document', 'artigo-12')).toBe('revisao-final.docx')
  })

  it('cria um nome determinístico quando a exportação não informa filename', () => {
    expect(nomeArquivoParaDownload(undefined, 'application/pdf', 'artigo-12')).toBe('artigo-12.pdf')
  })

  it('remove caminhos potencialmente perigosos e completa a extensão pelo MIME', () => {
    expect(nomeArquivoParaDownload('../../relatorio final', 'application/pdf', 'artigo-12')).toBe('relatorio final.pdf')
  })

  it('dispara o download com fallback quando o Blob não possui filename', () => {
    vi.useFakeTimers()
    const link = { href: '', download: '', style: { display: '' }, click: vi.fn(), remove: vi.fn() }
    const appendChild = vi.fn()
    vi.stubGlobal('document', { createElement: vi.fn(() => link), body: { appendChild } })
    vi.stubGlobal('URL', { createObjectURL: vi.fn(() => 'blob:artigo-12'), revokeObjectURL: vi.fn() })

    const nome = baixarBlob(new Blob(['pdf'], { type: 'application/pdf' }), undefined, 'artigo-12')

    expect(nome).toBe('artigo-12.pdf')
    expect(link.download).toBe('artigo-12.pdf')
    expect(link.click).toHaveBeenCalledOnce()
    expect(appendChild).toHaveBeenCalledWith(link)
    vi.runAllTimers()
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:artigo-12')
  })
})
