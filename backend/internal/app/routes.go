package app

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *App) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", a.handleHealth)
	mux.HandleFunc("GET /api/health", a.handleHealth)
	mux.HandleFunc("GET /api/info", handleInfo)
	mux.HandleFunc("POST /api/auth/login", a.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", a.handleLogout)
	mux.HandleFunc("GET /api/auth/me", a.handleMe)
	mux.HandleFunc("POST /api/governanca/registrar", a.handleGovernancaRegistrar)
	mux.HandleFunc("GET /api/governanca/dispositivos", a.handleGovernancaListar)
	mux.HandleFunc("DELETE /api/governanca/dispositivos/{id}", a.handleGovernancaRemover)
	mux.HandleFunc("POST /api/governanca/dispositivos/{id}/remover", a.handleGovernancaRemover)
	mux.HandleFunc("GET /api/governanca/status", a.handleGovernancaStatus)
	mux.HandleFunc("GET /api/artigos", a.handleListArtigos)
	mux.HandleFunc("POST /api/artigos", a.handleCreateArtigo)
	mux.HandleFunc("GET /api/artigos/{id}", a.handleGetArtigo)
	mux.HandleFunc("DELETE /api/artigos/{id}", a.handleDeleteArtigo)
	mux.HandleFunc("GET /api/artigos/{id}/paginas/{numero}/imagem", a.handlePaginaImagem)
	mux.HandleFunc("GET /api/artigos/{id}/paginas/{numero}/camada", a.handlePaginaCamada)
	mux.HandleFunc("GET /api/artigos/{id}/marcacoes", a.handleListMarcacoes)
	mux.HandleFunc("POST /api/artigos/{id}/marcacoes", a.handleCreateMarcacao)
	mux.HandleFunc("PATCH /api/artigos/{id}/marcacoes/{marcacao_id}", a.handleUpdateMarcacao)
	mux.HandleFunc("DELETE /api/artigos/{id}/marcacoes/{marcacao_id}", a.handleDeleteMarcacao)
	mux.HandleFunc("GET /api/artigos/{id}/notas", a.handleListNotas)
	mux.HandleFunc("POST /api/artigos/{id}/notas", a.handleCreateNota)
	mux.HandleFunc("PATCH /api/artigos/{id}/notas/{nota_id}", a.handleUpdateNota)
	mux.HandleFunc("DELETE /api/artigos/{id}/notas/{nota_id}", a.handleDeleteNota)
	mux.HandleFunc("GET /api/artigos/{id}/busca", a.handleBusca)
	mux.HandleFunc("GET /api/artigos/{id}/sumario", a.handleSumario)
	mux.HandleFunc("GET /api/artigos/{id}/historico", a.handleGetHistorico)
	mux.HandleFunc("POST /api/artigos/{id}/exportar", a.handleExportar)
	mux.HandleFunc("GET /api/artigos/{id}/exportar", a.handleDownloadExportar)
	mux.HandleFunc("GET /api/artigos/{id}/citacoes", a.handleListCitacoes)
	mux.HandleFunc("POST /api/artigos/{id}/varrer-citacoes", a.handleVarrerCitacoes)
	mux.HandleFunc("POST /api/artigos/{id}/citacoes/{citacao_id}/enriquecer", a.handleEnriquecerCitacao)
	mux.HandleFunc("POST /api/artigos/excluir-lote", a.handleExcluirLote)
	mux.HandleFunc("POST /api/sync", a.handleSync)
	mux.HandleFunc("GET /api/sync/pull", a.handleSyncPull)
	mux.HandleFunc("POST /api/sync/pull", a.handleSyncPull)
	mux.HandleFunc("POST /api/jobs", a.handleEnqueueJob)
	mux.HandleFunc("GET /api/jobs", a.handleListJobs)
	mux.HandleFunc("GET /api/jobs/{id}", a.handleGetJob)

	if a.WWWDir == "" {
		h := a.authMiddleware(mux)
		h = a.governancaMiddleware(h)
		h = ipadDetectorMiddleware(h)
		h = a.generalRateLimitMiddleware(h)
		h = bodyLimitMiddleware(h)
		h = withCORS(h)
		h = securityHeaders(h)
		h = recoveryMiddleware(h)
		return h
	}

	fileServer := http.FileServer(http.Dir(a.WWWDir))
	spa := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
			mux.ServeHTTP(w, r)
			return
		}
		// SPA fallback só para GET/HEAD; outros métodos não servem index.html
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		limpo := strings.TrimPrefix(filepath.Clean("/"+r.URL.Path), "/")
		alvo := filepath.Join(a.WWWDir, filepath.FromSlash(limpo))
		// trava path traversal: garante que alvo está dentro de WWWDir (com separador)
		if !strings.HasPrefix(alvo, a.WWWDir+string(os.PathSeparator)) && alvo != a.WWWDir {
			w.Header().Set("Cache-Control", "no-store")
			http.NotFound(w, r)
			return
		}
		if info, err := os.Stat(alvo); err == nil && !info.IsDir() {
			// cache estático: 1 dia para assets/favicon/icons, 1h para PWA precache (manifest/sw/workbox)
			if strings.HasPrefix(r.URL.Path, "/assets/") || r.URL.Path == "/favicon.ico" || strings.HasPrefix(r.URL.Path, "/icons/") {
				w.Header().Set("Cache-Control", "public, max-age=86400, immutable")
			} else if r.URL.Path == "/manifest.json" || r.URL.Path == "/manifest.webmanifest" || r.URL.Path == "/sw.js" || r.URL.Path == "/service-worker.js" || (strings.HasPrefix(r.URL.Path, "/workbox") && strings.HasSuffix(r.URL.Path, ".js")) {
				w.Header().Set("Cache-Control", "public, max-age=3600")
			}
			fileServer.ServeHTTP(w, r)
			return
		}
		// Se o path parece ser arquivo com extensão (ex: .js/.css/.png/.json) mas não existe, retorna 404 em vez de index.html
		// Isso evita que o SW ou navegador receba HTML no lugar de JS e quebre o boot sem mensagem.
		if ext := filepath.Ext(r.URL.Path); ext != "" {
			// exceção: .html sem arquivo pode cair no fallback SPA? Para SPA sem extensão, quer fallback; com extensão, é asset faltando.
			// Workaround: se for .html explícito e não existe, ainda faz fallback (SPA deep link vazia)
			if ext != ".html" && ext != ".htm" {
				w.Header().Set("Cache-Control", "no-store")
				http.NotFound(w, r)
				return
			}
		}
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFile(w, r, filepath.Join(a.WWWDir, "index.html"))
	})
	h := a.authMiddleware(spa)
	h = a.governancaMiddleware(h)
	h = ipadDetectorMiddleware(h)
	h = a.generalRateLimitMiddleware(h)
	h = bodyLimitMiddleware(h)
	h = withCORS(h)
	h = securityHeaders(h)
	h = recoveryMiddleware(h)
	return h
}
