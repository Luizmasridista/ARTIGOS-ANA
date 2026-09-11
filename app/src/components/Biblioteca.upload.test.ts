import { describe, expect, it } from 'vitest'
import { validarArquivoPDF } from './Biblioteca'

describe('Biblioteca - validarArquivoPDF', () => {
  it('nulo retorna escolha PDF', () => {
    expect(validarArquivoPDF(null)).toBe('Escolha um arquivo PDF.')
    expect(validarArquivoPDF(undefined)).toBe('Escolha um arquivo PDF.')
  })
  it('não-PDF retorna escolha PDF', () => {
    expect(validarArquivoPDF({ type: 'image/png', name: 'foto.png', size: 100 })).toBe('Escolha um arquivo PDF.')
  })
  it('tamanho 0 retorna mensagem iCloud', () => {
    const msg = validarArquivoPDF({ type: 'application/pdf', name: 'artigo.pdf', size: 0 })
    expect(msg).toContain('iCloud')
  })
  it('PDF válido retorna null', () => {
    expect(validarArquivoPDF({ type: 'application/pdf', name: 'artigo.pdf', size: 12345 })).toBeNull()
    expect(validarArquivoPDF({ type: '', name: 'ARTIGO.PDF', size: 10 })).toBeNull()
  })
})
