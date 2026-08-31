# language: pt
Funcionalidade: PWA iPad instalavel e offline
  Como usuario de iPad que le artigos
  Quero instalar o site na Tela de Inicio e usar offline
  Para ler artigos e ver marcacoes sem depender de rede

  Contexto:
    Dado que o backend esta online em http://127.0.0.1:8734
    E o frontend foi buildado com VitePWA gerando dist/manifest.json e dist/sw.js

  Cenário: Manifest valido instalavel
    Dado que abro GET https://artigos-ana.web.app/manifest.json
    Quando faco fetch de /manifest.json em http://127.0.0.1:4173/manifest.json
    Então recebo 200 com Content-Type application/json
    E o JSON tem name "Artigos Ana"
    E short_name "Artigos Ana"
    E start_url "/"
    E display "standalone"
    E scope "/"
    E background_color "#f7f7f5"
    E theme_color "#f7f7f5"
    E icons contem 192x192 com src "/icons/icon-192.png" e 512x512 com src "/icons/icon-512.png" ambos purpose "any maskable"
    E Cache-Control e "public, max-age=3600"

  Cenário: Service Worker registrado apenas em web nao no Electron
    Dado que estou em web sem window.artigosAna e com serviceWorker em navigator
    Quando chamo registerSW()
    Então navigator.serviceWorker.register("/sw.js") e chamado uma vez apos load
    E preflight adiciona listener updatefound e statechange sem quebrar
    Dado que estou em Electron com window.artigosAna definido
    Quando chamo registerSW()
    Então nenhum registro e feito
    E sem serviceWorker em navigator tambem nao quebra

  Cenário: sw.js precache e runtime conforme VitePWA
    Dado que faco fetch de /sw.js em http://127.0.0.1:4173/sw.js
    Então recebo 200 com Cache-Control "public, max-age=3600"
    E o corpo contem "precacheAndRoute" com index.html e assets e revision
    E contem "NavigationRoute" com fallback para index.html
    E contem StaleWhileRevalidate para "/api/artigos/.*\\/paginas/.*\\/camada" com cacheName api-camada-cache maxEntries 50 maxAge 604800
    E contem StaleWhileRevalidate para "/api/artigos" com cacheName api-artigos-cache maxEntries 50 maxAge 604800
    E contem NetworkOnly para POST /api/.*
    E contem NetworkOnly para PATCH /api/.*
    E contem NetworkOnly para DELETE /api/.*
    E tem skipWaiting e clientsClaim
    E tem precache com pelo menos 19 entradas incluindo manifest.webmanifest e icons

  Cenário: Offline precache dist funciona para artigo ja aberto
    Dado que abri o app em https://artigos-ana.web.app e naveguei ate /biblioteca e abri artigo 1
    E o SW fez precache de index.html e assets e runtime cache GET /api/artigos/1 e GET /api/artigos/1/paginas/1/camada
    Quando fico offline e recarrego a pagina
    Então o NavigationRoute serve index.html do cache
    E GET /api/artigos/1 retorna do cache StaleWhileRevalidate com status 200
    E GET /api/artigos/1/paginas/1/camada retorna do cache
    E vejo o artigo ja aberto e destaques anteriores sem tela branca

  Cenário: Runtime GET /api/artigos StaleWhileRevalidate e NetworkOnly para mutacoes
    Dado que estou online e faco GET /api/artigos
    Quando faco GET /api/artigos com Sw e depois offline faco GET /api/artigos novamente
    Então offline recebo resposta do cache api-artigos-cache
    Dado que tento POST /api/artigos com PDF estando offline
    Quando a requisicao vai via SW
    Então o SW usa NetworkOnly e a requisicao falha graciosamente sem salvar no cache
    E PATCH /api/artigos/1/marcacoes/5 tambem NetworkOnly falha graciosamente
    E DELETE /api/artigos/1/marcacoes/5 tambem NetworkOnly falha graciosamente
    E GET /api/artigos?busca=foo ainda usa StaleWhileRevalidate com maxEntries 50

  Cenário: Headers Cache-Control contrato
    Dado que faco GET /manifest.json no backend Go com -www dist
    Então Cache-Control e "public, max-age=3600" e nao contem no-store
    Dado que faco GET /sw.js no backend
    Então Cache-Control e "public, max-age=3600"
    Dado que faco GET /workbox-*.js
    Então Cache-Control e "public, max-age=3600"
    Dado que faco GET /assets/index-*.js
    Então Cache-Control e "public, max-age=86400, immutable"
    Dado que faco GET /api/artigos com cookie valido
    Então Cache-Control contem "no-store, no-cache, must-revalidate" e Pragma no-cache e Vary Origin
    Dado que faco GET /api/health
    Então Cache-Control contem "no-store" pois /api/* nunca e public
    Dado que faco GET /nao-existe rota SPA
    Então Cache-Control e "no-store" e serve index.html

  Cenário: Instalacao iPad Safari Add to Home Screen standalone
    Dado que abro Safari iPad em https://artigos-ana.web.app
    Quando toco Compartilhar e Adicionar a Tela de Inicio
    Então vejo icone icon-192.png e nome Artigos Ana
    E apos confirmar o icone abre sem barra de endereco
    E display e standalone e theme-color #f7f7f5 e apple-touch-icon presente
    E apple-mobile-web-app-capable e yes e viewport-fit cover preservados

  Cenário: Borda e invasivo manifest vazio corrompido e XSS
    Dado que faco fetch de /manifest.json que retorna JSON vazio {}
    Então validacao falha pois faltam name start_url display icons
    Dado que manifest contem name "<script>alert(1)</script>"
    Quando renderizo o nome no frontend como texto
    Então o texto aparece literal sem executar script e Content-Type permanece application/json com \u003c
    Dado que icons ausentes ou tamanhos errados 192 ou 512 faltando
    Então instalabilidade falha mas app nao quebra
    Dado que sw.js retorna 404
    Então registerSW catch silencioso nao quebra app web

  Cenário: Limites e race
    Dado que tenho 0 artigos e faco GET /api/artigos
    Quando valido headers
    Então ainda no-store
    Dado que tenho 100 artigos e faco GET /api/artigos
    Então ainda no-store e SW respeita maxEntries 50 expirando antigos
    Dado que ultrapasso MAX_INT em id artigo 9223372036854775807
    Quando faco GET /api/artigos/9223372036854775807/paginas/1/camada
    Então recebo 404 sem cachear
    Dado que faco 5 chamadas paralelas a registerSW()
    Quando todas resolvem no mesmo load
    Então cada chamada registra mas nao duplica erro e window.artigosAna ainda bloqueia
    Dado que Cache Storage atinge ~50MB
    Então SW expira conforme ExpirationPlugin maxEntries 50 maxAge 604800 e nao estoura quota
