# language: pt
Funcionalidade: Exclusão em lote + Fontes e Citações
  Como estudante que organiza artigos
  Quero excluir em lote escolhendo o que manter
  E ver fontes e citações organizadas por tipo com link para a web
  Para manter a biblioteca limpa e auditar referências

  Contexto:
    Dado que existem 3 artigos "A", "B", "C" na biblioteca
    E o backend está online em http://127.0.0.1:8734

  # Protocolo 1: BDD + Usabilidade + Segurança
  Cenário: Happy path - excluir em lote mantendo 1
    Dado que clico em "Excluir em lote"
    Então vejo dropdown com 3 checkboxes todos marcados
    Quando desmarco "B" para manter
    E clico em "Excluir selecionados"
    E confirmo "Excluir 2"
    Então vejo 1 artigo restante "B"
    E os arquivos de "A" e "C" foram removidos do disco

  Cenário: Nenhum selecionado desabilita confirmar
    Dado que abro dropdown de lote
    Quando desmarco todos
    Então o botão "Excluir selecionados" fica desabilitado

  Cenário: Race - múltiplos cliques rápidos
    Dado que seleciono 2 artigos
    Quando clico 5 vezes rápido em "Excluir selecionados"
    Então apenas 1 requisição POST /api/artigos/excluir-lote é processada
    E a lista final é consistente

  Cenário: Inputs vazios e corrompidos
    Quando envio POST /api/artigos/excluir-lote com {"ids":[]}
    Então recebo 400 "ids obrigatório"
    Quando envio {"ids":null}
    Então recebo 400
    Quando envio {"ids":["a"]}
    Então recebo 400
    Quando envio {"ids":[0]}
    Então recebo 400

  Cenário: Segurança - IDOR não apaga alheio
    Dado que tento enriquecer citação id=1 do artigo 1 usando artigo 2
    Então recebo 404 "citação não encontrada"

  Cenário: Biblioteca - XSS no título não executa
    Dado que um artigo tem título '<script>alert(1)</script> Meu Artigo'
    Quando abro a biblioteca
    Então vejo o texto literal "<script>alert(1)</script> Meu Artigo" sem executar script

  # Fontes & Citações
  Cenário: Happy path - varrer e listar organizadas
    Dado que abri o artigo 1 pela primeira vez
    Quando clico na aba "Fontes & Citações"
    Então o app chama GET /api/artigos/1/citacoes
    E se vazio, chama POST /api/artigos/1/varrer-citacoes exatamente uma vez
    E vejo grupos "Autor-data", "Numérica", "Referência" com contagem

  Cenário: Citação sem url mostra Buscar fonte
    Dado que uma citação não tem url
    Quando clico em "Buscar fonte"
    Então o app chama POST /api/artigos/1/citacoes/5/enriquecer
    E se retornar {"url":"https://example.com"} vejo "Abrir fonte"
    E se retornar {"url":""} vejo aviso "Nenhuma fonte encontrada"
    E múltiplos cliques rápidos não disparam segunda chamada (enriquecendoId)

  Cenário: Abrir fonte valida http/https
    Dado que uma citação tem url "https://example.com/artigo"
    Quando clico em "Abrir fonte"
    Então o IPC abrir-externo valida que o protocolo é http ou https e abre no navegador
    Quando a url é "javascript:alert(1)" ou "file:///etc/passwd"
    Então vejo aviso "URL inválida" e nada abre

  Cenário: Segurança - SSRF bloqueado no enriquecer
    Dado que o DuckDuckGo retorna <a href="http://127.0.0.1/private">evil</a>
    Quando enriqueço a citação
    Então recebo {"url":""} e o link privado não é salvo
    E para "http://10.0.0.1", "http://192.168.1.1", "http://169.254.169.254", "http://0.0.0.0", "file:///etc/passwd", "gopher://", "ftp://" também retorna vazio
    E redirect para IP privado é bloqueado

  Cenário: Segurança - Stored XSS nas citações não executa
    Dado que o PDF contém '<script>alert(1)</script> (Silva, 2020)'
    Quando varro as citações
    Então GET /api/artigos/1/citacoes retorna JSON com Content-Type application/json e payload escapado como \u003cscript
    E o frontend renderiza como texto literal sem executar

  Cenário: Segurança - SQLi não injeta
    Dado que o PDF contém "'; DROP TABLE citacoes; -- (Silva, 2020)"
    Quando varro as citações
    Então a tabela citacoes ainda existe e GET retorna 200

  Cenário: Limites e timeouts
    Dado que o DuckDuckGo demora mais que 8s ou retorna mais que 512KB
    Quando enriqueço
    Então recebo {"url":""} sem travar (timeout/best effort) e corpo é truncado em 512KB

  Cenário: Boundary - 1000 citações deduplicadas
    Dado que o texto contém "(Silva, 2020)" repetido 1000 vezes
    Quando varro
    Então recebo 1 citação única "(Silva, 2020)" (deduplicação por tipo|chave)

  Cenário: Concorrência - varrer em paralelo idempotente
    Quando disparo 10 POST /api/artigos/1/varrer-citacoes em paralelo
    Então todos retornam 200 com mesma quantidade e sem duplicar no banco
