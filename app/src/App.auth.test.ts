import { describe, expect, it } from 'vitest'
import { getRotaApp } from './App'

describe('App rota / → Home ou Biblioteca', () => {
  it('loading => loading', () => {
    expect(getRotaApp(null, true, null)).toBe('loading')
    expect(getRotaApp({ id: 1, nome: 'Ana' }, true, null)).toBe('loading')
  })
  it('sem user => home (mesmo sem artigo)', () => {
    expect(getRotaApp(null, false, null)).toBe('home')
    expect(getRotaApp(null, false, { id: 1 })).toBe('home')
  })
  it('com user sem artigo => biblioteca', () => {
    expect(getRotaApp({ id: 1, nome: 'Ana Bagatinii' }, false, null)).toBe('biblioteca')
  })
  it('com user com artigo => leitor', () => {
    expect(getRotaApp({ id: 1, nome: 'Ana' }, false, { id: 5 })).toBe('leitor')
  })
  it('Home é rota / quando não autenticado (sessão persiste via cookie)', () => {
    // simula que após reload, me retorna null => Home, se retorna user => Biblioteca
    const rotaSemSessao = getRotaApp(null, false, null)
    const rotaComSessao = getRotaApp({ id: 1, nome: 'Ana Bagatinii' }, false, null)
    expect(rotaSemSessao).toBe('home')
    expect(rotaComSessao).toBe('biblioteca')
  })
})
