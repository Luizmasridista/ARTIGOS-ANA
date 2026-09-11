package app

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"artigos-ana/backend/internal/store"
)

type App struct {
	DB         *store.Store
	DataDir    string
	PopplerDir string
	WWWDir     string

	pdfsDir    string
	paginasDir string
	exportDir  string
	tmpDir     string

	JWTSecret []byte
	limiter   sync.Map
	limiterMu sync.Mutex

	// sync / jobs concorrência (sem Redis, sem microserviços)
	syncSem    chan struct{}
	jobSem     chan struct{}
	workerStop chan struct{}
}

func New(dataDir, popplerDir, dbDSN, wwwDir string) (*App, error) {
	absData, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	absPoppler, err := filepath.Abs(popplerDir)
	if err != nil {
		return nil, err
	}
	a := &App{
		DataDir:    absData,
		PopplerDir: absPoppler,
		pdfsDir:    filepath.Join(absData, "pdfs"),
		paginasDir: filepath.Join(absData, "paginas"),
		exportDir:  filepath.Join(absData, "export"),
		tmpDir:     filepath.Join(absData, "tmp"),
	}
	if wwwDir != "" {
		absWWW, err := filepath.Abs(wwwDir)
		if err != nil {
			return nil, err
		}
		a.WWWDir = absWWW
	}
	for _, d := range []string{a.DataDir, a.pdfsDir, a.paginasDir, a.exportDir, a.tmpDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	st, err := store.Open(dbDSN)
	if err != nil {
		return nil, err
	}
	a.DB = st
	a.JWTSecret = loadJWTSecret()
	// concorrência configurável sem Redis: env SYNC_CONCURRENCY e JOB_CONCURRENCY
	syncConc := envInt("SYNC_CONCURRENCY", 4)
	if syncConc < 1 {
		syncConc = 1
	}
	if syncConc > 32 {
		syncConc = 32
	}
	a.syncSem = make(chan struct{}, syncConc)
	jobConc := envInt("JOB_CONCURRENCY", 2)
	if jobConc < 1 {
		jobConc = 1
	}
	if jobConc > 8 {
		jobConc = 8
	}
	a.jobSem = make(chan struct{}, jobConc)
	a.workerStop = make(chan struct{})
	// Recupera jobs que ficaram 'running' por queda/restart (Render free dorme):
	// voltam para 'pending' para o worker retomar (ex: upload interrompido).
	if res, err := st.DB().Exec(`UPDATE jobs SET status='pending', updated_at=now() WHERE status='running'`); err != nil {
		log.Printf("aviso: falha ao recuperar jobs running: %v", err)
	} else if n, _ := res.RowsAffected(); n > 0 {
		log.Printf("jobs recuperados para pending: %d", n)
	}
	go a.jobWorkerLoop()
	return a, nil
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func (a *App) tryAcquireSyncSlot() bool {
	select {
	case a.syncSem <- struct{}{}:
		return true
	default:
		return false
	}
}

func (a *App) releaseSyncSlot() {
	select {
	case <-a.syncSem:
	default:
	}
}

func (a *App) jobWorkerLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-a.workerStop:
			return
		case <-ticker.C:
			// tenta pegar até jobSem capacidade
			select {
			case a.jobSem <- struct{}{}:
			default:
				continue
			}
			job, err := a.DB.ClaimNextJob()
			if err != nil || job == nil {
				<-a.jobSem
				continue
			}
			go func(j *store.Job) {
				defer func() { <-a.jobSem }()
				a.processJob(j)
			}(job)
		}
	}
}

func (a *App) processJob(j *store.Job) {
	switch j.Tipo {
	case "noop", "teste", "":
		_ = a.DB.CompleteJob(j.ID, jsonRaw(`{"ok":true}`))
	case "processar_pdf":
		if err := a.processarPDFJob(j); err != nil {
			_ = a.DB.FailJob(j.ID, err.Error())
		} else {
			var in struct {
				ArtigoID int64 `json:"artigo_id"`
			}
			_ = json.Unmarshal(j.Payload, &in)
			_ = a.DB.CompleteJob(j.ID, jsonRaw(fmt.Sprintf(`{"ok":true,"artigo_id":%d}`, in.ArtigoID)))
		}
	default:
		// tipos não suportados nao devem ser marcados como sucesso falso
		_ = a.DB.FailJob(j.ID, "tipo não suportado: "+j.Tipo)
	}
}

func jsonRaw(s string) json.RawMessage { return json.RawMessage(s) }

func loadJWTSecret() []byte {
	sec := os.Getenv("JWT_SECRET")
	if len(sec) >= 32 {
		return []byte(sec)
	}
	if sec != "" {
		log.Printf("aviso: JWT_SECRET tem %d bytes (<32), gerando segredo aleatório para dev", len(sec))
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		// fallback
		for i := range b {
			b[i] = byte(i * 7)
		}
	}
	log.Printf("aviso: JWT_SECRET ausente ou curto, usando segredo aleatório em memória (defina JWT_SECRET em produção)")
	return b
}

func (a *App) Close() error {
	if a.workerStop != nil {
		close(a.workerStop)
	}
	return a.DB.Close()
}

func (a *App) pdfPath(id int64) string {
	return filepath.Join(a.pdfsDir, fmt.Sprintf("%d.pdf", id))
}

func (a *App) paginasPath(id int64) string {
	return filepath.Join(a.paginasDir, strconv.FormatInt(id, 10))
}

func (a *App) resolveDataFile(rel string) string {
	return filepath.Join(a.DataDir, filepath.FromSlash(rel))
}
