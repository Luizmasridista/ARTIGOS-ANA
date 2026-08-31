# CSS Modular — Artigos Ana

Monolito quebrado. Cada arquivo tem responsabilidade única.

| Arquivo | Responsabilidade |
|---------|------------------|
| `tokens.css` | Design tokens (`:root`, `[data-theme=dark]`) — cores, raios, espaçamentos, sombras, tipografia |
| `base.css` | Reset (`*`, `html,body,#root`, `body`, `button`, `::selection`, scrollbar, `.app`) |
| `buttons.css` | `.btn`, `.btn-primary`, `.btn-danger`, `.btn-ghost`, `.btn-sm`, `.btn-icon`, `.input`, `textarea` |
| `header.css` | `.header`, `.janela-controles`, `.janela-btn`, `.brand` |
| `biblioteca.css` | `.biblioteca`, `.biblioteca-inner`, `.lote-*`, `.dropzone`, `.lista-artigos`, `.artigo-card` |
| `leitor.css` | `.leitor`, `.leitor-toolbar`, `.leitor-paginas`, `.pagina`, `.pagina-img`, `.destaque-ret` |
| `tooltip.css` | `.tooltip-selecao`, `.tooltip-cor` |
| `popover.css` | `.popover-destaque`, `.popover-cor` |
| `painel.css` | `.painel`, `.painel-tabs`, `.nota-card`, `.nota-badge`, `.painel-backdrop`, `@media (max-width:1024px)` gaveta com `100dvh` — sem composer (movido) |
| `painel-ipad.css` | `html[data-device=ipad] .painel` — `100dvh` + `safe-area`, scroll `touch`, largura `85vw/360`, `backdrop` iPad |
| `ilha-nota.css` | `.ilha-nota` — prompt island flutuante bottom 16, max-width 640, shadow-2, r-l, safe-area, 44px iPad |
| `citacoes.css` | `.citacoes-*`, `.citacao-card`, `.fonte-destaque` |
| `dialog.css` | `.dialog-overlay`, `.dialog`, `.progresso` |
| `banner.css` | `.banner-offline`, `.toast`, `.leitor-erro` |
| `busca.css` | `.busca-mark`, `.biblioteca-contador` |
| `leitor-busca.css` | `.leitor-busca-barra`, `.busca-destaque` |
| `filtros.css` | `.painel-filtros`, `.chip-tag`, `.nota-tags` |
| `sumario.css` | `.sumario-lista`, `.sumario-item` |
| `home.css` | `.home`, `.home-card`, `.header-usuario` |
| `responsive.css` | `[data-modo=web]`, `@media (max-width:1024px)` e `640px` (`leitor-toolbar`, toque 40px; painel em `painel.css`) |
| `ipad.css` | `html[data-device=ipad]` — geral `dvh`, `safe-area`, `44px` HIG (painel específico em `painel-ipad.css`) |
| `grok.css` | `.grok-banner`, `.grok-status-dot`, `.grok-url-wrap` — hierarquia profissional |

## Regras para agentes (single responsibility)

1. **Um arquivo, um domínio.** Nunca misture `biblioteca` com `leitor` ou `tokens` com `header`. Se mexer em `Biblioteca.tsx`, edite só `biblioteca.css`.
2. **Tokens primeiro.** Cores, espaçamentos e sombras só em `tokens.css`. Componentes usam `var(--*)`, não hardcode.
3. **Import order** em `src/styles.css` importa na ordem acima — tokens → base → componentes → responsive → ipad → grok. Não reordene sem testar cascade.
4. **Componente novo → CSS novo.** Crie `src/styles/<nome>.css` ou `src/components/<Nome>.module.css` e importe em `styles.css` ou no componente. Nunca append 300 linhas no monolito.
5. **Editar preservando.** Antes de editar, leia o arquivo do domínio; mantenha o escopo mínimo.

## Como verificar

```bash
npm run build  # deve gerar dist/assets/index-*.css sem erro
npm test       # 143 testes
```

Se `styles.css` voltar a passar de 300 linhas, quebre.
