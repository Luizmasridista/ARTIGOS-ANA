package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"artigos-ana/backend/internal/app"
	"artigos-ana/backend/internal/store"
)

func main() {
	port := flag.Int("port", 8734, "porta HTTP")
	bind := flag.String("bind", "127.0.0.1", "endereço de bind (127.0.0.1 para local, 0.0.0.0 para rede)")
	dataDir := flag.String("data", "data", "diretório de dados")
	popplerDir := flag.String("poppler", "bin/poppler", "diretório dos binários poppler")
	wwwDir := flag.String("www", "../app/dist", "diretório estático do app web (vazio desativa)")
	dbURL := flag.String("db-url", "", "URL de conexão PostgreSQL (opcional; tem precedência sobre as variáveis de ambiente)")
	flag.Parse()
	if envBind := os.Getenv("BIND_ADDR"); envBind != "" && *bind == "127.0.0.1" {
		*bind = envBind
	}
	// Render Free injeta PORT=10000; tem precedencia sobre flag -port
	if portEnv := os.Getenv("PORT"); portEnv != "" {
		if p, err := strconv.Atoi(portEnv); err == nil && p > 0 && p < 65536 {
			*port = p
		} else {
			log.Printf("aviso: PORT env invalido %q, usando %d", portEnv, *port)
		}
	}
	// hardening: alerta se bind é 0.0.0.0 sem JWT_SECRET forte
	if *bind == "0.0.0.0" {
		sec := os.Getenv("JWT_SECRET")
		if len(sec) < 32 {
			log.Printf("ATENÇÃO: bind 0.0.0.0 expõe na rede mas JWT_SECRET tem %d bytes (<32). Defina JWT_SECRET com 32+ bytes em produção web!", len(sec))
		}
		if os.Getenv("ALLOWED_ORIGIN") == "" && os.Getenv("GROK_ORIGIN") == "" {
			log.Printf("ATENÇÃO: bind 0.0.0.0 sem ALLOWED_ORIGIN/GROK_ORIGIN — CORS em modo permissivo (desktop). Para web, defina ALLOWED_ORIGIN.")
		}
		log.Printf("AVISO: servidor exposto na rede (%s:%d). Certifique-se de firewall, TLS via GROK/proxy e autenticação.", *bind, *port)
	}

	cfg := store.ConfigFromEnv()
	// hardening: alerta de credencial padrão do banco em exposição web
	if *bind == "0.0.0.0" && cfg.Password == "Dudu1408@@" {
		log.Printf("ATENÇÃO: usando senha padrão do PostgreSQL em exposição web (PGPASSWORD). Defina PGPASSWORD forte via env!")
	}
	// Neon: aceita DATABASE_URL direto (postgres://user:pass@ep-xxx.neon.tech/db?sslmode=require)
	dbURLVal := *dbURL
	if dbURLVal == "" {
		dbURLVal = os.Getenv("DATABASE_URL")
	}
	dsn := store.BuildDSN(cfg, dbURLVal)
	if dbURLVal == "" {
		log.Printf("PostgreSQL: %s:%s banco=%s usuario=%s", cfg.Host, cfg.Port, cfg.Database, cfg.User)
	} else {
		log.Printf("PostgreSQL via DATABASE_URL (host derivado da URL)")
	}

	www := *wwwDir
	if www != "" {
		if _, err := os.Stat(www); err != nil {
			log.Printf("aviso: -www %s não encontrado; modo web desativado", www)
			www = ""
		}
	}

	a, err := app.New(*dataDir, *popplerDir, dsn, www)
	if err != nil {
		log.Fatalf("erro ao iniciar: %v", err)
	}
	defer a.Close()

	tlsCert := os.Getenv("TLS_CERT")
	tlsKey := os.Getenv("TLS_KEY")
	useTLS := tlsCert != "" && tlsKey != ""
	if useTLS {
		if _, err := os.Stat(tlsCert); err != nil {
			log.Fatalf("TLS_CERT %s não encontrado: %v", tlsCert, err)
		}
		if _, err := os.Stat(tlsKey); err != nil {
			log.Fatalf("TLS_KEY %s não encontrado: %v", tlsKey, err)
		}
		log.Printf("TLS ativado: cert=%s key=%s (https)", tlsCert, tlsKey)
	} else if *bind == "0.0.0.0" {
		log.Printf("AVISO CAPTURA: sem TLS (TLS_CERT/TLS_KEY vazios) tráfego em http puro pode ser capturado na rede local/WiFi. Use GROK https ou TLS para blindagem mínima.")
	}

	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", *bind, *port),
		Handler:           a.Routes(),
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20, // 1 MB
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	go func() {
		scheme := "http"
		if useTLS {
			scheme = "https"
		}
		if *bind == "0.0.0.0" {
			log.Printf("Artigos Ana backend ouvindo em %s://0.0.0.0:%d (acessível na rede)", scheme, *port)
		} else {
			log.Printf("Artigos Ana backend ouvindo em %s://%s:%d (apenas local)", scheme, *bind, *port)
		}
		if www != "" {
			log.Printf("app web disponível em %s://localhost:%d", scheme, *port)
		}
		var err error
		if useTLS {
			err = srv.ListenAndServeTLS(tlsCert, tlsKey)
		} else {
			err = srv.ListenAndServe()
		}
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("servidor: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("encerrando...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}
