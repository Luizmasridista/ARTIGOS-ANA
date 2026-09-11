package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS artigos (
		id BIGSERIAL PRIMARY KEY,
		titulo TEXT NOT NULL,
		arquivo_pdf TEXT NOT NULL,
		criado_em TIMESTAMPTZ NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS paginas (
		id BIGSERIAL PRIMARY KEY,
		artigo_id BIGINT NOT NULL REFERENCES artigos(id) ON DELETE CASCADE,
		numero INTEGER NOT NULL,
		imagem_png TEXT NOT NULL,
		camada_json JSONB NOT NULL,
		largura_px INTEGER NOT NULL,
		altura_px INTEGER NOT NULL,
		UNIQUE(artigo_id, numero)
	)`,
	`CREATE TABLE IF NOT EXISTS marcacoes (
		id BIGSERIAL PRIMARY KEY,
		artigo_id BIGINT NOT NULL REFERENCES artigos(id) ON DELETE CASCADE,
		pagina INTEGER NOT NULL,
		tipo TEXT NOT NULL,
		cor TEXT NOT NULL,
		palavras_json JSONB NOT NULL,
		texto TEXT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS notas (
		id BIGSERIAL PRIMARY KEY,
		artigo_id BIGINT NOT NULL REFERENCES artigos(id) ON DELETE CASCADE,
		pagina INTEGER NOT NULL,
		texto TEXT NOT NULL,
		criado_em TIMESTAMPTZ NOT NULL
	)`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS marcacao_id BIGINT`,
	`CREATE TABLE IF NOT EXISTS historico (
		id BIGSERIAL PRIMARY KEY,
		artigo_id BIGINT NOT NULL,
		entidade TEXT NOT NULL,
		entidade_id BIGINT,
		acao TEXT NOT NULL,
		dados JSONB NOT NULL,
		criado_em TIMESTAMPTZ NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS citacoes (
		id BIGSERIAL PRIMARY KEY,
		artigo_id BIGINT NOT NULL REFERENCES artigos(id) ON DELETE CASCADE,
		tipo TEXT NOT NULL,
		chave TEXT NOT NULL,
		autor TEXT,
		ano INTEGER,
		trecho TEXT,
		titulo TEXT,
		texto TEXT,
		url TEXT,
		criado_em TIMESTAMPTZ NOT NULL,
		pagina INTEGER DEFAULT 0,
		pos_json JSONB,
		ocorrencias_json JSONB
	)`,
	`CREATE INDEX IF NOT EXISTS idx_citacoes_artigo ON citacoes(artigo_id)`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS pagina INTEGER DEFAULT 0`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS pos_json JSONB`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS ocorrencias_json JSONB`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS tipo TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS chave TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS autor TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS ano INTEGER`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS trecho TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS titulo TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS texto TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS url TEXT`,
	`ALTER TABLE citacoes ADD COLUMN IF NOT EXISTS criado_em TIMESTAMPTZ`,
	`CREATE INDEX IF NOT EXISTS idx_paginas_artigo ON paginas(artigo_id)`,
	`CREATE INDEX IF NOT EXISTS idx_marcacoes_artigo ON marcacoes(artigo_id)`,
	`CREATE INDEX IF NOT EXISTS idx_notas_artigo ON notas(artigo_id)`,
	`CREATE INDEX IF NOT EXISTS idx_historico_artigo ON historico(artigo_id)`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS tags TEXT[] DEFAULT '{}'`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS cor TEXT DEFAULT '#FFEB3B'`,
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS pdf_data BYTEA`,
	`ALTER TABLE paginas ADD COLUMN IF NOT EXISTS imagem_data BYTEA`,
	`CREATE INDEX IF NOT EXISTS idx_notas_tags ON notas USING GIN (tags)`,
	`CREATE TABLE IF NOT EXISTS usuarios (
		id BIGSERIAL PRIMARY KEY,
		nome TEXT NOT NULL UNIQUE,
		senha_hash TEXT,
		criado_em TIMESTAMPTZ NOT NULL
	)`,
	`ALTER TABLE usuarios ALTER COLUMN senha_hash DROP NOT NULL`,
	`CREATE TABLE IF NOT EXISTS login_tentativas (
		id BIGSERIAL PRIMARY KEY,
		ip TEXT NOT NULL,
		tentativas INT NOT NULL,
		primeira_tentativa TIMESTAMPTZ NOT NULL,
		bloqueado_ate TIMESTAMPTZ,
		criado_em TIMESTAMPTZ NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_login_ip ON login_tentativas(ip)`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_login_ip_unique ON login_tentativas(ip)`,
	// --- offline-first / sync (idempotente, sem destruir dados legados) ---
	// artigos: colunas de sync (usuario_id, client_id, version, updated_at, deleted_at)
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS usuario_id BIGINT REFERENCES usuarios(id)`,
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS client_id TEXT`,
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1`,
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_artigos_client ON artigos(usuario_id, client_id) WHERE client_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_artigos_sync ON artigos(usuario_id, updated_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_artigos_usuario ON artigos(usuario_id)`,
	// marcacoes: sync
	`ALTER TABLE marcacoes ADD COLUMN IF NOT EXISTS usuario_id BIGINT REFERENCES usuarios(id)`,
	`ALTER TABLE marcacoes ADD COLUMN IF NOT EXISTS client_id TEXT`,
	`ALTER TABLE marcacoes ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1`,
	`ALTER TABLE marcacoes ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
	`ALTER TABLE marcacoes ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_marcacoes_client ON marcacoes(usuario_id, client_id) WHERE client_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_marcacoes_sync ON marcacoes(usuario_id, updated_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_marcacoes_usuario ON marcacoes(usuario_id)`,
	// notas: sync
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS usuario_id BIGINT REFERENCES usuarios(id)`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS client_id TEXT`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS version BIGINT NOT NULL DEFAULT 1`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT now()`,
	`ALTER TABLE notas ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ`,
	`CREATE UNIQUE INDEX IF NOT EXISTS idx_notas_client ON notas(usuario_id, client_id) WHERE client_id IS NOT NULL`,
	`CREATE INDEX IF NOT EXISTS idx_notas_sync ON notas(usuario_id, updated_at, id)`,
	`CREATE INDEX IF NOT EXISTS idx_notas_usuario ON notas(usuario_id)`,
	// idempotencia por usuario+op_id (deviceId/opId/clientId)
	`CREATE TABLE IF NOT EXISTS sync_operations (
		id BIGSERIAL PRIMARY KEY,
		usuario_id BIGINT NOT NULL REFERENCES usuarios(id) ON DELETE CASCADE,
		op_id TEXT NOT NULL,
		device_id TEXT,
		response JSONB NOT NULL,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(usuario_id, op_id)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_sync_ops_usuario ON sync_operations(usuario_id)`,
	// fila de jobs (worker limitado no processo, SKIP LOCKED) - isolada por usuario_id
	`CREATE TABLE IF NOT EXISTS jobs (
		id BIGSERIAL PRIMARY KEY,
		tipo TEXT NOT NULL,
		payload JSONB NOT NULL,
		status TEXT NOT NULL DEFAULT 'pending',
		attempts INT NOT NULL DEFAULT 0,
		max_attempts INT NOT NULL DEFAULT 5,
		next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		result JSONB,
		error_text TEXT
	)`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_status_next ON jobs(status, next_attempt_at, id)`,
	`ALTER TABLE jobs ADD COLUMN IF NOT EXISTS usuario_id BIGINT REFERENCES usuarios(id)`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_usuario ON jobs(usuario_id)`,
	`CREATE INDEX IF NOT EXISTS idx_jobs_usuario_status ON jobs(usuario_id, status, next_attempt_at, id)`,
	// --- persistencia Render Free (Neon): pdf + imagens em BYTEA para sobreviver ao disco efemero ---
	`ALTER TABLE artigos ADD COLUMN IF NOT EXISTS pdf_data BYTEA`,
	`ALTER TABLE paginas ADD COLUMN IF NOT EXISTS imagem_data BYTEA`,
	// --- governanca: dispositivos/IPs autorizados (hash SHA256(serial+JWT_SECRET), sem serial em claro) ---
	`CREATE TABLE IF NOT EXISTS dispositivos_autorizados (
		id BIGSERIAL PRIMARY KEY,
		identificador TEXT NOT NULL,
		tipo TEXT NOT NULL,
		descricao TEXT,
		criado_em TIMESTAMPTZ NOT NULL DEFAULT now(),
		UNIQUE(identificador, tipo)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_dispositivos_tipo ON dispositivos_autorizados(tipo)`,
	`CREATE INDEX IF NOT EXISTS idx_dispositivos_identificador ON dispositivos_autorizados(identificador)`,
}

type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	Database string
}

func ConfigFromEnv() Config {
	return Config{
		Host:     getenv("PGHOST", "localhost"),
		Port:     getenv("PGPORT", "5432"),
		User:     getenv("PGUSER", "postgres"),
		Password: getenv("PGPASSWORD", "Dudu1408@@"),
		Database: getenv("PGDATABASE", "artigos_ana"),
	}
}

func BuildDSN(cfg Config, dbURL string) string {
	if dbURL != "" {
		return dbURL
	}
	u := &url.URL{
		Scheme: "postgres",
		User:   url.UserPassword(cfg.User, cfg.Password),
		Host:   net.JoinHostPort(cfg.Host, cfg.Port),
		Path:   "/" + cfg.Database,
	}
	q := u.Query()
	q.Set("sslmode", "disable")
	u.RawQuery = q.Encode()
	return u.String()
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("abrir postgres: %w", err)
	}
	db.SetMaxOpenConns(10)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("conectar postgres: %w", err)
	}
	for _, stmt := range schemaStatements {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			return nil, fmt.Errorf("criar schema: %w", err)
		}
	}
	st := &Store{db: db}
	if err := st.MigrateCamadasLegadas(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrar camadas legadas: %w", err)
	}
	if err := st.ensureUsuarioAna(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed usuario ana: %w", err)
	}
	if err := st.backfillSyncColumns(); err != nil {
		db.Close()
		return nil, fmt.Errorf("backfill sync: %w", err)
	}
	return st, nil
}

const fatorCamadaLegada = 150.0 / 72.0

type camadaPalavra struct {
	Texto string  `json:"texto"`
	X0    float64 `json:"x0"`
	Y0    float64 `json:"y0"`
	X1    float64 `json:"x1"`
	Y1    float64 `json:"y1"`
}

func MigrateCamadasLegadas(db *sql.DB) error {
	if err := migrarPaginasLegadas(db); err != nil {
		return err
	}
	return migrarMarcacoesLegadas(db)
}

func (s *Store) MigrateCamadasLegadas() error {
	return MigrateCamadasLegadas(s.db)
}

func migrarPaginasLegadas(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, camada_json FROM paginas`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type atual struct {
		id  int64
		raw []byte
	}
	var updates []atual
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var palavras []camadaPalavra
		if err := json.Unmarshal(raw, &palavras); err != nil || len(palavras) == 0 {
			continue
		}
		maxX := 0.0
		for _, p := range palavras {
			if p.X1 > maxX {
				maxX = p.X1
			}
		}
		if maxX >= 700 {
			continue
		}
		for i := range palavras {
			palavras[i].X0 *= fatorCamadaLegada
			palavras[i].Y0 *= fatorCamadaLegada
			palavras[i].X1 *= fatorCamadaLegada
			palavras[i].Y1 *= fatorCamadaLegada
		}
		novo, err := json.Marshal(palavras)
		if err != nil {
			return err
		}
		updates = append(updates, atual{id: id, raw: novo})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := db.Exec(`UPDATE paginas SET camada_json = $1 WHERE id = $2`, string(u.raw), u.id); err != nil {
			return err
		}
	}
	return nil
}

func migrarMarcacoesLegadas(db *sql.DB) error {
	rows, err := db.Query(`SELECT id, palavras_json FROM marcacoes`)
	if err != nil {
		return err
	}
	defer rows.Close()
	type atual struct {
		id  int64
		raw []byte
	}
	var updates []atual
	for rows.Next() {
		var id int64
		var raw []byte
		if err := rows.Scan(&id, &raw); err != nil {
			return err
		}
		var caixas [][]float64
		if err := json.Unmarshal(raw, &caixas); err != nil || len(caixas) == 0 {
			continue
		}
		maxX := 0.0
		for _, c := range caixas {
			if len(c) == 4 && c[2] > maxX {
				maxX = c[2]
			}
		}
		if maxX >= 700 {
			continue
		}
		for i := range caixas {
			for j := range caixas[i] {
				caixas[i][j] *= fatorCamadaLegada
			}
		}
		novo, err := json.Marshal(caixas)
		if err != nil {
			return err
		}
		updates = append(updates, atual{id: id, raw: novo})
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := db.Exec(`UPDATE marcacoes SET palavras_json = $1 WHERE id = $2`, string(u.raw), u.id); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ensureUsuarioAna() error {
	// Seed dos 2 usuários fixos sem senha (login por seleção, sem slop)
	for _, nome := range []string{"Ana Bagatinii", "Luiz"} {
		var exists bool
		err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM usuarios WHERE nome = $1)`, nome).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			continue
		}
		_, err = s.db.Exec(`INSERT INTO usuarios (nome, senha_hash, criado_em) VALUES ($1, '', now()) ON CONFLICT (nome) DO NOTHING`, nome)
		if err != nil {
			return err
		}
	}
	// Garante que senha_hash pode ser vazio (sem senha)
	_, _ = s.db.Exec(`UPDATE usuarios SET senha_hash = '' WHERE senha_hash IS NULL`)
	return nil
}

// backfillSyncColumns preenche colunas de sync para registros legados.
// Usa usuario Ana como dono padrão para não destruir dados existentes.
// É idempotente: só atualiza onde usuario_id IS NULL ou client_id IS NULL.
func (s *Store) backfillSyncColumns() error {
	var anaID int64
	err := s.db.QueryRow(`SELECT id FROM usuarios WHERE nome = 'Ana Bagatinii'`).Scan(&anaID)
	if err != nil {
		// se não existir Ana, não faz backfill (seed falhou? não bloqueia startup)
		return nil
	}
	// artigos
	_, _ = s.db.Exec(`UPDATE artigos SET usuario_id = $1 WHERE usuario_id IS NULL`, anaID)
	_, _ = s.db.Exec(`UPDATE artigos SET client_id = 'legacy-artigo-' || id::text WHERE client_id IS NULL`)
	_, _ = s.db.Exec(`UPDATE artigos SET updated_at = COALESCE(updated_at, criado_em, now()) WHERE updated_at IS NULL`)
	// marcacoes
	_, _ = s.db.Exec(`UPDATE marcacoes SET usuario_id = $1 WHERE usuario_id IS NULL`, anaID)
	_, _ = s.db.Exec(`UPDATE marcacoes SET client_id = 'legacy-marcacao-' || id::text WHERE client_id IS NULL`)
	// notas
	_, _ = s.db.Exec(`UPDATE notas SET usuario_id = $1 WHERE usuario_id IS NULL`, anaID)
	_, _ = s.db.Exec(`UPDATE notas SET client_id = 'legacy-nota-' || id::text WHERE client_id IS NULL`)
	_, _ = s.db.Exec(`UPDATE notas SET updated_at = COALESCE(updated_at, criado_em, now()) WHERE updated_at IS NULL`)
	// version já default 1 via schema, mas garante para linhas antigas que talvez tenham 0
	_, _ = s.db.Exec(`UPDATE artigos SET version = 1 WHERE version IS NULL OR version < 1`)
	_, _ = s.db.Exec(`UPDATE marcacoes SET version = 1 WHERE version IS NULL OR version < 1`)
	_, _ = s.db.Exec(`UPDATE notas SET version = 1 WHERE version IS NULL OR version < 1`)
	// jobs: backfill usuario_id para jobs legados (nullable -> Ana)
	_, _ = s.db.Exec(`UPDATE jobs SET usuario_id = $1 WHERE usuario_id IS NULL`, anaID)
	return nil
}

type Usuario struct {
	ID        int64
	Nome      string
	SenhaHash string
	CriadoEm  time.Time
}

func (s *Store) GetUsuarioByNome(nome string) (*Usuario, error) {
	var u Usuario
	err := s.db.QueryRow(`SELECT id, nome, senha_hash, criado_em FROM usuarios WHERE nome = $1`, nome).Scan(&u.ID, &u.Nome, &u.SenhaHash, &u.CriadoEm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUsuarioByID(id int64) (*Usuario, error) {
	var u Usuario
	err := s.db.QueryRow(`SELECT id, nome, senha_hash, criado_em FROM usuarios WHERE id = $1`, id).Scan(&u.ID, &u.Nome, &u.SenhaHash, &u.CriadoEm)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

// SetUsuarioSenha grava o hash bcrypt da senha (nunca a senha em claro).
func (s *Store) SetUsuarioSenha(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE usuarios SET senha_hash = $1 WHERE id = $2`, hash, id)
	return err
}

// SetArtigoTitulo atualiza o título (usado ao extrair o título do paper no worker).
func (s *Store) SetArtigoTitulo(id int64, titulo string) error {
	_, err := s.db.Exec(`UPDATE artigos SET titulo = $1 WHERE id = $2`, titulo, id)
	return err
}

type LoginTentativa struct {
	ID                int64
	IP                string
	Tentativas        int
	PrimeiraTentativa time.Time
	BloqueadoAte      *time.Time
	CriadoEm          time.Time
}

func (s *Store) GetLoginTentativa(ip string) (*LoginTentativa, error) {
	var lt LoginTentativa
	var bloqueado sql.NullTime
	var criado time.Time
	err := s.db.QueryRow(`SELECT id, ip, tentativas, primeira_tentativa, bloqueado_ate, criado_em FROM login_tentativas WHERE ip = $1`, ip).Scan(&lt.ID, &lt.IP, &lt.Tentativas, &lt.PrimeiraTentativa, &bloqueado, &criado)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	lt.CriadoEm = criado
	if bloqueado.Valid {
		lt.BloqueadoAte = &bloqueado.Time
	}
	return &lt, nil
}

func (s *Store) UpsertLoginTentativa(ip string, tentativas int, primeiraTentativa time.Time, bloqueadoAte *time.Time) error {
	_, err := s.db.Exec(`
		INSERT INTO login_tentativas (ip, tentativas, primeira_tentativa, bloqueado_ate, criado_em)
		VALUES ($1, $2, $3, $4, now())
		ON CONFLICT (ip) DO UPDATE SET tentativas = EXCLUDED.tentativas, primeira_tentativa = EXCLUDED.primeira_tentativa, bloqueado_ate = EXCLUDED.bloqueado_ate
	`, ip, tentativas, primeiraTentativa, bloqueadoAte)
	return err
}

func (s *Store) DeleteLoginTentativa(ip string) error {
	_, err := s.db.Exec(`DELETE FROM login_tentativas WHERE ip = $1`, ip)
	return err
}

func (s *Store) Close() error {
	return s.db.Close()
}

type Artigo struct {
	ID         int64  `json:"id"`
	Titulo     string `json:"titulo"`
	CriadoEm   string `json:"criado_em"`
	NumPaginas int    `json:"num_paginas,omitempty"`
}

type ArtigoDetalhe struct {
	ID       int64          `json:"id"`
	Titulo   string         `json:"titulo"`
	CriadoEm string         `json:"criado_em"`
	Paginas  []PaginaResumo `json:"paginas"`
}

type PaginaResumo struct {
	Numero  int `json:"numero"`
	Largura int `json:"largura"`
	Altura  int `json:"altura"`
}

type Pagina struct {
	ID         int64
	ArtigoID   int64
	Numero     int
	ImagemPNG  string
	CamadaJSON json.RawMessage
	LarguraPx  int
	AlturaPx   int
}

type Marcacao struct {
	ID           int64           `json:"id"`
	Pagina       int             `json:"pagina"`
	Tipo         string          `json:"tipo"`
	Cor          string          `json:"cor"`
	PalavrasJSON json.RawMessage `json:"palavras"`
	Texto        string          `json:"texto"`
}

type Nota struct {
	ID         int64    `json:"id"`
	Pagina     int      `json:"pagina"`
	Texto      string   `json:"texto"`
	CriadoEm   string   `json:"criado_em"`
	MarcacaoID *int64   `json:"marcacao_id,omitempty"`
	Tags       []string `json:"tags"`
	Cor        string   `json:"cor"`
}

type Historico struct {
	ID         int64           `json:"id"`
	Entidade   string          `json:"entidade"`
	EntidadeID *int64          `json:"entidade_id,omitempty"`
	Acao       string          `json:"acao"`
	Dados      json.RawMessage `json:"dados"`
	CriadoEm   string          `json:"criado_em"`
}

func now() time.Time {
	return time.Now()
}

func fmtTime(t time.Time) string {
	return t.Format(time.RFC3339)
}

func (s *Store) CreateArtigo(titulo, arquivoPDF string) (int64, string, error) {
	criado := now()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO artigos (titulo, arquivo_pdf, criado_em) VALUES ($1, $2, $3) RETURNING id`,
		titulo, arquivoPDF, criado).Scan(&id); err != nil {
		return 0, "", err
	}
	// compat: artigos legados diretos via teste recebem dono Ana para nao quebrar isolamento
	var anaID sql.NullInt64
	_ = tx.QueryRow(`SELECT id FROM usuarios WHERE nome = 'Ana Bagatinii'`).Scan(&anaID)
	if anaID.Valid {
		_, _ = tx.Exec(`UPDATE artigos SET usuario_id = $2, client_id = COALESCE(client_id, 'legacy-artigo-' || $1::text), version = COALESCE(version,1), updated_at = COALESCE(updated_at, now()) WHERE id = $1 AND usuario_id IS NULL`, id, anaID.Int64)
	} else {
		_, _ = tx.Exec(`UPDATE artigos SET client_id = COALESCE(client_id, 'legacy-artigo-' || $1::text) WHERE id = $1 AND client_id IS NULL`, id)
	}
	if err := addHistoricoTx(tx, id, "artigo", id, "criar", map[string]any{"titulo": titulo}); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return id, fmtTime(criado), nil
}

func (s *Store) SetArtigoPDF(id int64, arquivoPDF string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE artigos SET arquivo_pdf = $1 WHERE id = $2`, arquivoPDF, id); err != nil {
		return err
	}
	if err := addHistoricoTx(tx, id, "artigo", id, "atualizar", map[string]any{"arquivo_pdf": arquivoPDF}); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Store) GetArtigo(id int64) (Artigo, bool, error) {
	var a Artigo
	var criado time.Time
	err := s.db.QueryRow(`SELECT id, titulo, criado_em FROM artigos WHERE id = $1`, id).
		Scan(&a.ID, &a.Titulo, &criado)
	if err == sql.ErrNoRows {
		return Artigo{}, false, nil
	}
	if err != nil {
		return Artigo{}, false, err
	}
	a.CriadoEm = fmtTime(criado)
	return a, true, nil
}

func (s *Store) ListArtigos() ([]Artigo, error) {
	rows, err := s.db.Query(`
		SELECT a.id, a.titulo, a.criado_em, (SELECT COUNT(*) FROM paginas p WHERE p.artigo_id = a.id)
		FROM artigos a ORDER BY a.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Artigo
	for rows.Next() {
		var a Artigo
		var criado time.Time
		if err := rows.Scan(&a.ID, &a.Titulo, &criado, &a.NumPaginas); err != nil {
			return nil, err
		}
		a.CriadoEm = fmtTime(criado)
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) DeleteArtigo(id int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var titulo, arquivoPDF string
	err = tx.QueryRow(`SELECT titulo, arquivo_pdf FROM artigos WHERE id = $1`, id).Scan(&titulo, &arquivoPDF)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := addHistoricoTx(tx, id, "artigo", id, "excluir", map[string]any{"titulo": titulo, "arquivo_pdf": arquivoPDF}); err != nil {
		return false, err
	}
	res, err := tx.Exec(`DELETE FROM artigos WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// --- Scoped (multiusuario) helpers: nao quebram metodos legados ---

func (s *Store) ListArtigosByUser(busca string, usuarioID int64) ([]Artigo, error) {
	busca = strings.TrimSpace(busca)
	if busca == "" {
		rows, err := s.db.Query(`
			SELECT a.id, a.titulo, a.criado_em, (SELECT COUNT(*) FROM paginas p WHERE p.artigo_id = a.id)
			FROM artigos a WHERE (a.usuario_id = $1 OR (a.usuario_id IS NULL AND $1 = (SELECT id FROM usuarios WHERE nome='Ana Bagatinii'))) AND a.deleted_at IS NULL ORDER BY a.id DESC`, usuarioID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var list []Artigo
		for rows.Next() {
			var a Artigo
			var criado time.Time
			if err := rows.Scan(&a.ID, &a.Titulo, &criado, &a.NumPaginas); err != nil {
				return nil, err
			}
			a.CriadoEm = fmtTime(criado)
			list = append(list, a)
		}
		return list, rows.Err()
	}
	if len([]rune(busca)) > 200 {
		busca = string([]rune(busca)[:200])
	}
	buscaEsc := escapeLike(busca)
	rows, err := s.db.Query(`
		SELECT a.id, a.titulo, a.criado_em, (SELECT COUNT(*) FROM paginas p WHERE p.artigo_id = a.id)
		FROM artigos a
		WHERE (a.usuario_id = $1 OR (a.usuario_id IS NULL AND $1 = (SELECT id FROM usuarios WHERE nome='Ana Bagatinii'))) AND a.deleted_at IS NULL
		  AND (a.titulo ILIKE '%' || $2 || '%' ESCAPE '\'
		  OR EXISTS (SELECT 1 FROM notas n WHERE n.artigo_id = a.id AND (n.usuario_id = $1 OR n.usuario_id IS NULL) AND n.deleted_at IS NULL AND (n.texto ILIKE '%' || $2 || '%' ESCAPE '\' OR EXISTS (SELECT 1 FROM unnest(n.tags) t WHERE t ILIKE '%' || $2 || '%' ESCAPE '\')))
		  OR EXISTS (SELECT 1 FROM citacoes c WHERE c.artigo_id = a.id AND (c.autor ILIKE '%' || $2 || '%' ESCAPE '\' OR c.chave ILIKE '%' || $2 || '%' ESCAPE '\')))
		ORDER BY a.id DESC`, usuarioID, buscaEsc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Artigo
	for rows.Next() {
		var a Artigo
		var criado time.Time
		if err := rows.Scan(&a.ID, &a.Titulo, &criado, &a.NumPaginas); err != nil {
			return nil, err
		}
		a.CriadoEm = fmtTime(criado)
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) GetArtigoByUser(id, usuarioID int64) (Artigo, bool, error) {
	var a Artigo
	var criado time.Time
	err := s.db.QueryRow(`SELECT id, titulo, criado_em FROM artigos WHERE id = $1 AND (usuario_id = $2 OR (usuario_id IS NULL AND $2 = (SELECT id FROM usuarios WHERE nome='Ana Bagatinii'))) AND deleted_at IS NULL`, id, usuarioID).
		Scan(&a.ID, &a.Titulo, &criado)
	if err == sql.ErrNoRows {
		return Artigo{}, false, nil
	}
	if err != nil {
		return Artigo{}, false, err
	}
	a.CriadoEm = fmtTime(criado)
	return a, true, nil
}

func (s *Store) CreateArtigoForUser(titulo, arquivoPDF string, usuarioID int64) (int64, string, error) {
	criado := now()
	tx, err := s.db.Begin()
	if err != nil {
		return 0, "", err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO artigos (titulo, arquivo_pdf, criado_em, usuario_id, version, updated_at) VALUES ($1, $2, $3, $4, 1, now()) RETURNING id`,
		titulo, arquivoPDF, criado, usuarioID).Scan(&id); err != nil {
		return 0, "", err
	}
	if _, err := tx.Exec(`UPDATE artigos SET client_id = 'legacy-artigo-' || $1::text WHERE id = $1 AND client_id IS NULL`, id); err != nil {
		return 0, "", err
	}
	if err := addHistoricoTx(tx, id, "artigo", id, "criar", map[string]any{"titulo": titulo}); err != nil {
		return 0, "", err
	}
	if err := tx.Commit(); err != nil {
		return 0, "", err
	}
	return id, fmtTime(criado), nil
}

func (s *Store) ArtigoOwnedBy(artigoID, usuarioID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM artigos WHERE id = $1 AND (usuario_id = $2 OR (usuario_id IS NULL AND $2 = (SELECT id FROM usuarios WHERE nome='Ana Bagatinii'))) AND deleted_at IS NULL)`, artigoID, usuarioID).Scan(&exists)
	return exists, err
}

func (s *Store) CanAccessArticle(artigoID, usuarioID int64) (bool, error) {
	return s.ArtigoOwnedBy(artigoID, usuarioID)
}

func (s *Store) CreateMarcacaoForUser(artigoID, usuarioID int64, pagina int, tipo, cor string, palavrasJSON []byte, texto string) (Marcacao, error) {
	if palavrasJSON == nil {
		palavrasJSON = []byte("[]")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return Marcacao{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO marcacoes (artigo_id, usuario_id, pagina, tipo, cor, palavras_json, texto, version, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,1,now()) RETURNING id`,
		artigoID, usuarioID, pagina, tipo, cor, string(palavrasJSON), texto).Scan(&id); err != nil {
		return Marcacao{}, err
	}
	if _, err := tx.Exec(`UPDATE marcacoes SET client_id = 'legacy-marcacao-' || $1::text WHERE id = $1 AND client_id IS NULL`, id); err != nil {
		return Marcacao{}, err
	}
	if err := addHistoricoTx(tx, artigoID, "marcacao", id, "criar", map[string]any{
		"pagina":   pagina,
		"tipo":     tipo,
		"cor":      cor,
		"palavras": json.RawMessage(palavrasJSON),
		"texto":    texto,
	}); err != nil {
		return Marcacao{}, err
	}
	if err := tx.Commit(); err != nil {
		return Marcacao{}, err
	}
	return Marcacao{ID: id, Pagina: pagina, Tipo: tipo, Cor: cor, PalavrasJSON: json.RawMessage(palavrasJSON), Texto: texto}, nil
}

func (s *Store) CreateNotaComTagsForUser(artigoID, usuarioID int64, pagina int, texto string, marcacaoID *int64, tags []string, cor string) (Nota, error) {
	if tags == nil {
		tags = []string{}
	}
	if cor == "" {
		cor = "#FFEB3B"
	}
	tagsStr := encodeTags(tags)
	criado := now()
	tx, err := s.db.Begin()
	if err != nil {
		return Nota{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO notas (artigo_id, usuario_id, pagina, texto, criado_em, marcacao_id, tags, cor, version, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7::TEXT[],$8,1,now()) RETURNING id`,
		artigoID, usuarioID, pagina, texto, criado, marcacaoID, tagsStr, cor).Scan(&id); err != nil {
		return Nota{}, err
	}
	if _, err := tx.Exec(`UPDATE notas SET client_id = 'legacy-nota-' || $1::text WHERE id = $1 AND client_id IS NULL`, id); err != nil {
		return Nota{}, err
	}
	dados := map[string]any{"pagina": pagina, "texto": texto, "tags": tags, "cor": cor}
	if marcacaoID != nil {
		dados["marcacao_id"] = *marcacaoID
	}
	if err := addHistoricoTx(tx, artigoID, "nota", id, "criar", dados); err != nil {
		return Nota{}, err
	}
	if err := tx.Commit(); err != nil {
		return Nota{}, err
	}
	return Nota{ID: id, Pagina: pagina, Texto: texto, CriadoEm: fmtTime(criado), MarcacaoID: marcacaoID, Tags: tags, Cor: cor}, nil
}

func (s *Store) AddPagina(artigoID int64, numero int, imagemPNG string, palavrasJSON []byte, larguraPx, alturaPx int) error {
	_, err := s.db.Exec(`INSERT INTO paginas (artigo_id, numero, imagem_png, camada_json, largura_px, altura_px) VALUES ($1, $2, $3, $4, $5, $6)`,
		artigoID, numero, imagemPNG, string(palavrasJSON), larguraPx, alturaPx)
	return err
}

func (s *Store) ListPaginas(artigoID int64) ([]Pagina, error) {
	rows, err := s.db.Query(`SELECT id, artigo_id, numero, imagem_png, camada_json, largura_px, altura_px FROM paginas WHERE artigo_id = $1 ORDER BY numero`, artigoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Pagina
	for rows.Next() {
		var p Pagina
		var camada []byte
		if err := rows.Scan(&p.ID, &p.ArtigoID, &p.Numero, &p.ImagemPNG, &camada, &p.LarguraPx, &p.AlturaPx); err != nil {
			return nil, err
		}
		p.CamadaJSON = json.RawMessage(camada)
		list = append(list, p)
	}
	return list, rows.Err()
}

func (s *Store) GetPagina(artigoID int64, numero int) (Pagina, bool, error) {
	var p Pagina
	var camada []byte
	err := s.db.QueryRow(`SELECT id, artigo_id, numero, imagem_png, camada_json, largura_px, altura_px FROM paginas WHERE artigo_id = $1 AND numero = $2`, artigoID, numero).
		Scan(&p.ID, &p.ArtigoID, &p.Numero, &p.ImagemPNG, &camada, &p.LarguraPx, &p.AlturaPx)
	if err == sql.ErrNoRows {
		return Pagina{}, false, nil
	}
	if err != nil {
		return Pagina{}, false, err
	}
	p.CamadaJSON = json.RawMessage(camada)
	return p, true, nil
}

func (s *Store) ListMarcacoes(artigoID int64) ([]Marcacao, error) {
	rows, err := s.db.Query(`SELECT id, pagina, tipo, cor, palavras_json, texto FROM marcacoes WHERE artigo_id = $1 ORDER BY pagina, id`, artigoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Marcacao
	for rows.Next() {
		var m Marcacao
		var palavras []byte
		if err := rows.Scan(&m.ID, &m.Pagina, &m.Tipo, &m.Cor, &palavras, &m.Texto); err != nil {
			return nil, err
		}
		m.PalavrasJSON = json.RawMessage(palavras)
		list = append(list, m)
	}
	return list, rows.Err()
}

func (s *Store) CreateMarcacao(artigoID int64, pagina int, tipo, cor string, palavrasJSON []byte, texto string) (Marcacao, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Marcacao{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO marcacoes (artigo_id, pagina, tipo, cor, palavras_json, texto) VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		artigoID, pagina, tipo, cor, string(palavrasJSON), texto).Scan(&id); err != nil {
		return Marcacao{}, err
	}
	var anaID sql.NullInt64
	_ = tx.QueryRow(`SELECT id FROM usuarios WHERE nome = 'Ana Bagatinii'`).Scan(&anaID)
	var artigoUID sql.NullInt64
	_ = tx.QueryRow(`SELECT usuario_id FROM artigos WHERE id = $1`, artigoID).Scan(&artigoUID)
	owner := anaID
	if artigoUID.Valid {
		owner = artigoUID
	}
	if owner.Valid {
		_, _ = tx.Exec(`UPDATE marcacoes SET usuario_id = $2, client_id = COALESCE(client_id, 'legacy-marcacao-' || $1::text), version = COALESCE(version,1), updated_at = COALESCE(updated_at, now()) WHERE id = $1 AND usuario_id IS NULL`, id, owner.Int64)
	} else {
		_, _ = tx.Exec(`UPDATE marcacoes SET client_id = COALESCE(client_id, 'legacy-marcacao-' || $1::text) WHERE id = $1 AND client_id IS NULL`, id)
	}
	if err := addHistoricoTx(tx, artigoID, "marcacao", id, "criar", map[string]any{
		"pagina":   pagina,
		"tipo":     tipo,
		"cor":      cor,
		"palavras": json.RawMessage(palavrasJSON),
		"texto":    texto,
	}); err != nil {
		return Marcacao{}, err
	}
	if err := tx.Commit(); err != nil {
		return Marcacao{}, err
	}
	return Marcacao{ID: id, Pagina: pagina, Tipo: tipo, Cor: cor, PalavrasJSON: json.RawMessage(palavrasJSON), Texto: texto}, nil
}

func (s *Store) DeleteMarcacao(artigoID, marcacaoID int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var pagina int
	var tipo, cor, texto string
	var palavras []byte
	err = tx.QueryRow(`SELECT pagina, tipo, cor, palavras_json, texto FROM marcacoes WHERE id = $1 AND artigo_id = $2`, marcacaoID, artigoID).
		Scan(&pagina, &tipo, &cor, &palavras, &texto)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := addHistoricoTx(tx, artigoID, "marcacao", marcacaoID, "excluir", map[string]any{
		"pagina":   pagina,
		"tipo":     tipo,
		"cor":      cor,
		"palavras": json.RawMessage(palavras),
		"texto":    texto,
	}); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM marcacoes WHERE id = $1 AND artigo_id = $2`, marcacaoID, artigoID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) UpdateMarcacaoCor(artigoID, marcacaoID int64, cor string) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE marcacoes SET cor = $1 WHERE id = $2 AND artigo_id = $3`, cor, marcacaoID, artigoID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if err := addHistoricoTx(tx, artigoID, "marcacao", marcacaoID, "atualizar", map[string]any{"cor": cor}); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func encodeTags(tags []string) string {
	if len(tags) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{")
	for i, t := range tags {
		if i > 0 {
			b.WriteString(",")
		}
		t = strings.ReplaceAll(t, `\`, `\\`)
		t = strings.ReplaceAll(t, `"`, `\"`)
		b.WriteString(`"`)
		b.WriteString(t)
		b.WriteString(`"`)
	}
	b.WriteString("}")
	return b.String()
}

func decodeTags(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return []string{}
	}
	if strings.HasPrefix(raw, "{") && strings.HasSuffix(raw, "}") {
		raw = raw[1 : len(raw)-1]
	} else {
		return []string{}
	}
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var out []string
	var cur strings.Builder
	inQuote := false
	escaped := false
	for _, r := range raw {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if r == ',' && !inQuote {
			s := strings.TrimSpace(cur.String())
			if s != "" {
				out = append(out, s)
			}
			cur.Reset()
			continue
		}
		cur.WriteRune(r)
	}
	s := strings.TrimSpace(cur.String())
	if s != "" {
		out = append(out, s)
	}
	if out == nil {
		return []string{}
	}
	return out
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func (s *Store) ListNotas(artigoID int64) ([]Nota, error) {
	return s.ListNotasFiltered(artigoID, "", "")
}

func (s *Store) ListNotasFiltered(artigoID int64, tagFilter, corFilter string) ([]Nota, error) {
	query := `SELECT id, pagina, texto, criado_em, marcacao_id, COALESCE(tags::TEXT, '{}'), COALESCE(cor, '#FFEB3B') FROM notas WHERE artigo_id = $1`
	args := []any{artigoID}
	if tagFilter != "" {
		query += ` AND $` + strconv.Itoa(len(args)+1) + ` = ANY(tags)`
		args = append(args, tagFilter)
	}
	if corFilter != "" {
		query += ` AND cor = $` + strconv.Itoa(len(args)+1)
		args = append(args, corFilter)
	}
	query += ` ORDER BY id`
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Nota
	for rows.Next() {
		var n Nota
		var criado time.Time
		var marcacaoID sql.NullInt64
		var tagsRaw sql.NullString
		var cor sql.NullString
		if err := rows.Scan(&n.ID, &n.Pagina, &n.Texto, &criado, &marcacaoID, &tagsRaw, &cor); err != nil {
			return nil, err
		}
		n.CriadoEm = fmtTime(criado)
		if marcacaoID.Valid {
			n.MarcacaoID = &marcacaoID.Int64
		}
		if tagsRaw.Valid {
			n.Tags = decodeTags(tagsRaw.String)
		} else {
			n.Tags = []string{}
		}
		if n.Tags == nil {
			n.Tags = []string{}
		}
		if cor.Valid {
			n.Cor = cor.String
		} else {
			n.Cor = "#FFEB3B"
		}
		list = append(list, n)
	}
	return list, rows.Err()
}

func (s *Store) GetNota(artigoID, notaID int64) (Nota, bool, error) {
	var n Nota
	var criado time.Time
	var marcacaoID sql.NullInt64
	var tagsRaw sql.NullString
	var cor sql.NullString
	err := s.db.QueryRow(`SELECT id, pagina, texto, criado_em, marcacao_id, COALESCE(tags::TEXT, '{}'), COALESCE(cor, '#FFEB3B') FROM notas WHERE id = $1 AND artigo_id = $2`, notaID, artigoID).
		Scan(&n.ID, &n.Pagina, &n.Texto, &criado, &marcacaoID, &tagsRaw, &cor)
	if err == sql.ErrNoRows {
		return Nota{}, false, nil
	}
	if err != nil {
		return Nota{}, false, err
	}
	n.CriadoEm = fmtTime(criado)
	if marcacaoID.Valid {
		n.MarcacaoID = &marcacaoID.Int64
	}
	if tagsRaw.Valid {
		n.Tags = decodeTags(tagsRaw.String)
	} else {
		n.Tags = []string{}
	}
	if n.Tags == nil {
		n.Tags = []string{}
	}
	if cor.Valid {
		n.Cor = cor.String
	} else {
		n.Cor = "#FFEB3B"
	}
	return n, true, nil
}

func (s *Store) CreateNota(artigoID int64, pagina int, texto string, marcacaoID *int64) (Nota, error) {
	return s.CreateNotaComTags(artigoID, pagina, texto, marcacaoID, []string{}, "#FFEB3B")
}

func (s *Store) CreateNotaComTags(artigoID int64, pagina int, texto string, marcacaoID *int64, tags []string, cor string) (Nota, error) {
	if tags == nil {
		tags = []string{}
	}
	if cor == "" {
		cor = "#FFEB3B"
	}
	tagsStr := encodeTags(tags)
	criado := now()
	tx, err := s.db.Begin()
	if err != nil {
		return Nota{}, err
	}
	defer tx.Rollback()
	var id int64
	if err := tx.QueryRow(`INSERT INTO notas (artigo_id, pagina, texto, criado_em, marcacao_id, tags, cor) VALUES ($1, $2, $3, $4, $5, $6::TEXT[], $7) RETURNING id`,
		artigoID, pagina, texto, criado, marcacaoID, tagsStr, cor).Scan(&id); err != nil {
		return Nota{}, err
	}
	var anaID sql.NullInt64
	_ = tx.QueryRow(`SELECT id FROM usuarios WHERE nome = 'Ana Bagatinii'`).Scan(&anaID)
	var artigoUID sql.NullInt64
	_ = tx.QueryRow(`SELECT usuario_id FROM artigos WHERE id = $1`, artigoID).Scan(&artigoUID)
	owner := anaID
	if artigoUID.Valid {
		owner = artigoUID
	}
	if owner.Valid {
		_, _ = tx.Exec(`UPDATE notas SET usuario_id = $2, client_id = COALESCE(client_id, 'legacy-nota-' || $1::text), version = COALESCE(version,1), updated_at = COALESCE(updated_at, now()) WHERE id = $1 AND usuario_id IS NULL`, id, owner.Int64)
	} else {
		_, _ = tx.Exec(`UPDATE notas SET client_id = COALESCE(client_id, 'legacy-nota-' || $1::text) WHERE id = $1 AND client_id IS NULL`, id)
	}
	dados := map[string]any{"pagina": pagina, "texto": texto, "tags": tags, "cor": cor}
	if marcacaoID != nil {
		dados["marcacao_id"] = *marcacaoID
	}
	if err := addHistoricoTx(tx, artigoID, "nota", id, "criar", dados); err != nil {
		return Nota{}, err
	}
	if err := tx.Commit(); err != nil {
		return Nota{}, err
	}
	return Nota{ID: id, Pagina: pagina, Texto: texto, CriadoEm: fmtTime(criado), MarcacaoID: marcacaoID, Tags: tags, Cor: cor}, nil
}

func (s *Store) UpdateNota(artigoID, notaID int64, texto *string, tags *[]string, cor *string) (Nota, bool, error) {
	n, found, err := s.GetNota(artigoID, notaID)
	if err != nil {
		return Nota{}, false, err
	}
	if !found {
		return Nota{}, false, nil
	}
	set := []string{}
	args := []any{}
	idx := 1
	dados := map[string]any{}
	if texto != nil {
		set = append(set, "texto = $"+strconv.Itoa(idx))
		args = append(args, *texto)
		idx++
		n.Texto = *texto
		dados["texto"] = *texto
	}
	if tags != nil {
		tagsStr := encodeTags(*tags)
		set = append(set, "tags = $"+strconv.Itoa(idx)+"::TEXT[]")
		args = append(args, tagsStr)
		idx++
		n.Tags = *tags
		dados["tags"] = *tags
	}
	if cor != nil {
		set = append(set, "cor = $"+strconv.Itoa(idx))
		args = append(args, *cor)
		idx++
		n.Cor = *cor
		dados["cor"] = *cor
	}
	if len(set) == 0 {
		return n, true, nil
	}
	args = append(args, notaID, artigoID)
	query := "UPDATE notas SET " + strings.Join(set, ", ") + " WHERE id = $" + strconv.Itoa(idx) + " AND artigo_id = $" + strconv.Itoa(idx+1)
	tx, err := s.db.Begin()
	if err != nil {
		return Nota{}, false, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(query, args...)
	if err != nil {
		return Nota{}, false, err
	}
	aff, err := res.RowsAffected()
	if err != nil {
		return Nota{}, false, err
	}
	if aff == 0 {
		return Nota{}, false, nil
	}
	if err := addHistoricoTx(tx, artigoID, "nota", notaID, "atualizar", dados); err != nil {
		return Nota{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return Nota{}, false, err
	}
	return n, true, nil
}

func (s *Store) DeleteNota(artigoID, notaID int64) (bool, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	var pagina int
	var texto string
	err = tx.QueryRow(`SELECT pagina, texto FROM notas WHERE id = $1 AND artigo_id = $2`, notaID, artigoID).
		Scan(&pagina, &texto)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if err := addHistoricoTx(tx, artigoID, "nota", notaID, "excluir", map[string]any{"pagina": pagina, "texto": texto}); err != nil {
		return false, err
	}
	if _, err := tx.Exec(`DELETE FROM notas WHERE id = $1 AND artigo_id = $2`, notaID, artigoID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (s *Store) ListArtigosFiltered(busca string) ([]Artigo, error) {
	busca = strings.TrimSpace(busca)
	if busca == "" {
		return s.ListArtigos()
	}
	if len([]rune(busca)) > 200 {
		busca = string([]rune(busca)[:200])
	}
	buscaEsc := escapeLike(busca)
	rows, err := s.db.Query(`
		SELECT a.id, a.titulo, a.criado_em, (SELECT COUNT(*) FROM paginas p WHERE p.artigo_id = a.id)
		FROM artigos a
		WHERE a.titulo ILIKE '%' || $1 || '%' ESCAPE '\'
		  OR EXISTS (SELECT 1 FROM notas n WHERE n.artigo_id = a.id AND (n.texto ILIKE '%' || $1 || '%' ESCAPE '\' OR EXISTS (SELECT 1 FROM unnest(n.tags) t WHERE t ILIKE '%' || $1 || '%' ESCAPE '\')))
		  OR EXISTS (SELECT 1 FROM citacoes c WHERE c.artigo_id = a.id AND (c.autor ILIKE '%' || $1 || '%' ESCAPE '\' OR c.chave ILIKE '%' || $1 || '%' ESCAPE '\'))
		ORDER BY a.id DESC`, buscaEsc)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Artigo
	for rows.Next() {
		var a Artigo
		var criado time.Time
		if err := rows.Scan(&a.ID, &a.Titulo, &criado, &a.NumPaginas); err != nil {
			return nil, err
		}
		a.CriadoEm = fmtTime(criado)
		list = append(list, a)
	}
	return list, rows.Err()
}

func (s *Store) AddHistorico(artigoID int64, entidade string, entidadeID int64, acao string, dados any) error {
	dadosJSON, err := json.Marshal(dados)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO historico (artigo_id, entidade, entidade_id, acao, dados, criado_em) VALUES ($1, $2, $3, $4, $5, $6)`,
		artigoID, entidade, entidadeID, acao, string(dadosJSON), time.Now())
	return err
}

func addHistoricoTx(tx *sql.Tx, artigoID int64, entidade string, entidadeID int64, acao string, dados any) error {
	dadosJSON, err := json.Marshal(dados)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO historico (artigo_id, entidade, entidade_id, acao, dados, criado_em) VALUES ($1, $2, $3, $4, $5, $6)`,
		artigoID, entidade, entidadeID, acao, string(dadosJSON), time.Now())
	return err
}

func (s *Store) ListHistorico(artigoID int64) ([]Historico, error) {
	rows, err := s.db.Query(`SELECT id, entidade, entidade_id, acao, dados, criado_em FROM historico WHERE artigo_id = $1 ORDER BY id DESC`, artigoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Historico
	for rows.Next() {
		var h Historico
		var entidadeID sql.NullInt64
		var dados []byte
		var criado time.Time
		if err := rows.Scan(&h.ID, &h.Entidade, &entidadeID, &h.Acao, &dados, &criado); err != nil {
			return nil, err
		}
		if entidadeID.Valid {
			h.EntidadeID = &entidadeID.Int64
		}
		h.Dados = json.RawMessage(dados)
		h.CriadoEm = fmtTime(criado)
		list = append(list, h)
	}
	return list, rows.Err()
}

type Citacao struct {
	ID          int64           `json:"id"`
	ArtigoID    int64           `json:"-"`
	Tipo        string          `json:"tipo"`
	Chave       string          `json:"chave"`
	Autor       string          `json:"autor"`
	Ano         *int            `json:"ano"`
	Trecho      string          `json:"trecho"`
	Titulo      string          `json:"titulo"`
	Texto       string          `json:"texto"`
	URL         string          `json:"url"`
	CriadoEm    string          `json:"criado_em"`
	Pagina      int             `json:"pagina"`
	Pos         json.RawMessage `json:"pos"`
	Ocorrencias json.RawMessage `json:"ocorrencias"`
}

func (s *Store) ListCitacoes(artigoID int64) ([]Citacao, error) {
	rows, err := s.db.Query(`SELECT id, artigo_id, tipo, chave, autor, ano, trecho, titulo, texto, url, criado_em, pagina, pos_json, ocorrencias_json FROM citacoes WHERE artigo_id = $1 ORDER BY id`, artigoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var list []Citacao
	for rows.Next() {
		var c Citacao
		var autor, trecho, titulo, texto, urlVal sql.NullString
		var ano sql.NullInt32
		var criado time.Time
		var pagina sql.NullInt32
		var posJSON, ocorrJSON []byte
		if err := rows.Scan(&c.ID, &c.ArtigoID, &c.Tipo, &c.Chave, &autor, &ano, &trecho, &titulo, &texto, &urlVal, &criado, &pagina, &posJSON, &ocorrJSON); err != nil {
			return nil, err
		}
		if autor.Valid {
			c.Autor = autor.String
		}
		if ano.Valid {
			v := int(ano.Int32)
			c.Ano = &v
		}
		if trecho.Valid {
			c.Trecho = trecho.String
		}
		if titulo.Valid {
			c.Titulo = titulo.String
		}
		if texto.Valid {
			c.Texto = texto.String
		}
		if urlVal.Valid {
			c.URL = urlVal.String
		}
		c.CriadoEm = fmtTime(criado)
		if pagina.Valid {
			c.Pagina = int(pagina.Int32)
		}
		if posJSON != nil {
			c.Pos = json.RawMessage(posJSON)
		}
		if ocorrJSON != nil {
			c.Ocorrencias = json.RawMessage(ocorrJSON)
		}
		list = append(list, c)
	}
	return list, rows.Err()
}

func (s *Store) GetCitacao(artigoID, citacaoID int64) (Citacao, bool, error) {
	var c Citacao
	var autor, trecho, titulo, texto, urlVal sql.NullString
	var ano sql.NullInt32
	var criado time.Time
	var pagina sql.NullInt32
	var posJSON, ocorrJSON []byte
	err := s.db.QueryRow(`SELECT id, artigo_id, tipo, chave, autor, ano, trecho, titulo, texto, url, criado_em, pagina, pos_json, ocorrencias_json FROM citacoes WHERE id = $1 AND artigo_id = $2`, citacaoID, artigoID).
		Scan(&c.ID, &c.ArtigoID, &c.Tipo, &c.Chave, &autor, &ano, &trecho, &titulo, &texto, &urlVal, &criado, &pagina, &posJSON, &ocorrJSON)
	if err == sql.ErrNoRows {
		return Citacao{}, false, nil
	}
	if err != nil {
		return Citacao{}, false, err
	}
	if autor.Valid {
		c.Autor = autor.String
	}
	if ano.Valid {
		v := int(ano.Int32)
		c.Ano = &v
	}
	if trecho.Valid {
		c.Trecho = trecho.String
	}
	if titulo.Valid {
		c.Titulo = titulo.String
	}
	if texto.Valid {
		c.Texto = texto.String
	}
	if urlVal.Valid {
		c.URL = urlVal.String
	}
	c.CriadoEm = fmtTime(criado)
	if pagina.Valid {
		c.Pagina = int(pagina.Int32)
	}
	if posJSON != nil {
		c.Pos = json.RawMessage(posJSON)
	}
	if ocorrJSON != nil {
		c.Ocorrencias = json.RawMessage(ocorrJSON)
	}
	return c, true, nil
}

func (s *Store) ReplaceCitacoes(artigoID int64, items []Citacao) ([]Citacao, error) {
	// Preserva URLs existentes por tipo|chave antes de apagar
	existentes, err := s.ListCitacoes(artigoID)
	if err != nil {
		return nil, err
	}
	urlMap := map[string]string{}
	for _, e := range existentes {
		if e.URL != "" {
			urlMap[e.Tipo+"|"+e.Chave] = e.URL
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM citacoes WHERE artigo_id = $1`, artigoID); err != nil {
		return nil, err
	}
	for _, it := range items {
		criado := now()
		urlVal := it.URL
		if urlVal == "" {
			if v, ok := urlMap[it.Tipo+"|"+it.Chave]; ok {
				urlVal = v
			}
		}
		var anoVal any
		if it.Ano != nil {
			anoVal = *it.Ano
		}
		var autorVal any
		if it.Autor != "" {
			autorVal = it.Autor
		}
		var posVal any
		if len(it.Pos) > 0 && string(it.Pos) != "null" {
			posVal = string(it.Pos)
		}
		var ocorrVal any
		if len(it.Ocorrencias) > 0 && string(it.Ocorrencias) != "null" {
			ocorrVal = string(it.Ocorrencias)
		}
		if _, err := tx.Exec(`INSERT INTO citacoes (artigo_id, tipo, chave, autor, ano, trecho, titulo, texto, url, criado_em, pagina, pos_json, ocorrencias_json) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
			artigoID, it.Tipo, it.Chave, autorVal, anoVal, it.Trecho, it.Titulo, it.Texto, urlVal, criado, it.Pagina, posVal, ocorrVal); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.ListCitacoes(artigoID)
}

func (s *Store) UpdateCitacaoURL(artigoID, citacaoID int64, urlStr string) (bool, error) {
	res, err := s.db.Exec(`UPDATE citacoes SET url = $1 WHERE id = $2 AND artigo_id = $3`, urlStr, citacaoID, artigoID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// ==================== SYNC (offline-first) ====================
// Limite de isolamento: as tabelas legadas (notas/marcacoes/artigos) permanecem acessíveis via endpoints legados sem filtro por usuario_id para compatibilidade.
// O caminho novo de sync (/api/sync) é isolado por usuario_id (derivado do JWT) e garante idempotência + LWW.
// Documentado aqui para não inventar segurança além do escopo: isolamento completo dos endpoints legados exigiria reescrever todos os handlers para filtrar por usuario_id e migrar dados multi-usuário, o que está fora desta fatia.

func (s *Store) DB() *sql.DB { return s.db }

type SyncOpResult struct {
	ID        int64           `json:"id"`
	UsuarioID int64           `json:"-"`
	OpID      string          `json:"-"`
	DeviceID  string          `json:"-"`
	Response  json.RawMessage `json:"-"`
}

func (s *Store) GetSyncOperation(usuarioID int64, opID string) (json.RawMessage, bool, error) {
	var resp []byte
	err := s.db.QueryRow(`SELECT response FROM sync_operations WHERE usuario_id = $1 AND op_id = $2`, usuarioID, opID).Scan(&resp)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return json.RawMessage(resp), true, nil
}

func (s *Store) PutSyncOperationTx(tx *sql.Tx, usuarioID int64, opID, deviceID string, response json.RawMessage) error {
	_, err := tx.Exec(`INSERT INTO sync_operations (usuario_id, op_id, device_id, response) VALUES ($1,$2,$3,$4) ON CONFLICT (usuario_id, op_id) DO NOTHING`, usuarioID, opID, deviceID, string(response))
	return err
}

// Notas sync helpers

type NotaSync struct {
	ID         int64      `json:"id"`
	ArtigoID   int64      `json:"artigo_id"`
	UsuarioID  int64      `json:"-"`
	ClientID   string     `json:"client_id"`
	Pagina     int        `json:"pagina"`
	Texto      string     `json:"texto"`
	MarcacaoID *int64     `json:"marcacao_id,omitempty"`
	Tags       []string   `json:"tags"`
	Cor        string     `json:"cor"`
	Version    int64      `json:"version"`
	UpdatedAt  time.Time  `json:"updated_at"`
	DeletedAt  *time.Time `json:"deleted_at,omitempty"`
	Deleted    bool       `json:"deleted"`
	RawTags    string     `json:"-"`
}

func (s *Store) GetNotaSyncByIDTx(tx *sql.Tx, id, usuarioID int64) (*NotaSync, error) {
	var n NotaSync
	var marcacaoID sql.NullInt64
	var tagsRaw sql.NullString
	var cor sql.NullString
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT id, artigo_id, usuario_id, COALESCE(client_id,''), pagina, texto, marcacao_id, COALESCE(tags::TEXT,'{}'), COALESCE(cor,'#FFEB3B'), version, updated_at, deleted_at FROM notas WHERE id = $1 AND usuario_id = $2`, id, usuarioID).
		Scan(&n.ID, &n.ArtigoID, &n.UsuarioID, &n.ClientID, &n.Pagina, &n.Texto, &marcacaoID, &tagsRaw, &cor, &n.Version, &n.UpdatedAt, &deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if marcacaoID.Valid {
		n.MarcacaoID = &marcacaoID.Int64
	}
	if tagsRaw.Valid {
		n.Tags = decodeTags(tagsRaw.String)
	}
	if cor.Valid {
		n.Cor = cor.String
	}
	if deletedAt.Valid {
		n.DeletedAt = &deletedAt.Time
		n.Deleted = true
	}
	return &n, nil
}

func (s *Store) GetNotaSyncByClientIDTx(tx *sql.Tx, clientID string, usuarioID int64) (*NotaSync, error) {
	var n NotaSync
	var marcacaoID sql.NullInt64
	var tagsRaw sql.NullString
	var cor sql.NullString
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT id, artigo_id, usuario_id, COALESCE(client_id,''), pagina, texto, marcacao_id, COALESCE(tags::TEXT,'{}'), COALESCE(cor,'#FFEB3B'), version, updated_at, deleted_at FROM notas WHERE client_id = $1 AND usuario_id = $2`, clientID, usuarioID).
		Scan(&n.ID, &n.ArtigoID, &n.UsuarioID, &n.ClientID, &n.Pagina, &n.Texto, &marcacaoID, &tagsRaw, &cor, &n.Version, &n.UpdatedAt, &deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if marcacaoID.Valid {
		n.MarcacaoID = &marcacaoID.Int64
	}
	if tagsRaw.Valid {
		n.Tags = decodeTags(tagsRaw.String)
	}
	if cor.Valid {
		n.Cor = cor.String
	}
	if deletedAt.Valid {
		n.DeletedAt = &deletedAt.Time
		n.Deleted = true
	}
	return &n, nil
}

func (s *Store) InsertNotaSyncTx(tx *sql.Tx, usuarioID, artigoID int64, clientID string, pagina int, texto string, marcacaoID *int64, tags []string, cor string) (*NotaSync, error) {
	if tags == nil {
		tags = []string{}
	}
	if cor == "" {
		cor = "#FFEB3B"
	}
	tagsStr := encodeTags(tags)
	var id int64
	var version int64
	var updatedAt time.Time
	err := tx.QueryRow(`INSERT INTO notas (artigo_id, usuario_id, client_id, pagina, texto, criado_em, marcacao_id, tags, cor, version, updated_at) VALUES ($1,$2,$3,$4,$5,now(),$6,$7::TEXT[],$8,1,now()) RETURNING id, version, updated_at`,
		artigoID, usuarioID, clientID, pagina, texto, marcacaoID, tagsStr, cor).Scan(&id, &version, &updatedAt)
	if err != nil {
		return nil, err
	}
	return &NotaSync{ID: id, ArtigoID: artigoID, UsuarioID: usuarioID, ClientID: clientID, Pagina: pagina, Texto: texto, MarcacaoID: marcacaoID, Tags: tags, Cor: cor, Version: version, UpdatedAt: updatedAt}, nil
}

func (s *Store) UpdateNotaSyncTx(tx *sql.Tx, id, usuarioID int64, pagina *int, texto *string, marcacaoID *int64, tags *[]string, cor *string) (*NotaSync, error) {
	// LWW: incrementa version e updated_at = now() (relógio do servidor, não do cliente)
	set := []string{"version = version + 1", "updated_at = now()"}
	args := []any{}
	idx := 1
	if pagina != nil {
		set = append(set, "pagina = $"+strconv.Itoa(idx))
		args = append(args, *pagina)
		idx++
	}
	if texto != nil {
		set = append(set, "texto = $"+strconv.Itoa(idx))
		args = append(args, *texto)
		idx++
	}
	if marcacaoID != nil {
		set = append(set, "marcacao_id = $"+strconv.Itoa(idx))
		args = append(args, *marcacaoID)
		idx++
	}
	if tags != nil {
		set = append(set, "tags = $"+strconv.Itoa(idx)+"::TEXT[]")
		args = append(args, encodeTags(*tags))
		idx++
	}
	if cor != nil {
		set = append(set, "cor = $"+strconv.Itoa(idx))
		args = append(args, *cor)
		idx++
	}
	args = append(args, id, usuarioID)
	query := "UPDATE notas SET " + strings.Join(set, ", ") + " WHERE id = $" + strconv.Itoa(idx) + " AND usuario_id = $" + strconv.Itoa(idx+1) + " AND deleted_at IS NULL RETURNING version, updated_at"
	var version int64
	var updatedAt time.Time
	if err := tx.QueryRow(query, args...).Scan(&version, &updatedAt); err != nil {
		return nil, err
	}
	return &NotaSync{ID: id, Version: version, UpdatedAt: updatedAt}, nil
}

func (s *Store) SoftDeleteNotaSyncTx(tx *sql.Tx, id, usuarioID int64) (int64, time.Time, error) {
	var version int64
	var updatedAt time.Time
	err := tx.QueryRow(`UPDATE notas SET deleted_at = now(), updated_at = now(), version = version + 1 WHERE id = $1 AND usuario_id = $2 AND deleted_at IS NULL RETURNING version, updated_at`, id, usuarioID).Scan(&version, &updatedAt)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, sql.ErrNoRows
	}
	return version, updatedAt, err
}

// Marcacoes sync helpers

type MarcacaoSync struct {
	ID        int64           `json:"id"`
	ArtigoID  int64           `json:"artigo_id"`
	UsuarioID int64           `json:"-"`
	ClientID  string          `json:"client_id"`
	Pagina    int             `json:"pagina"`
	Tipo      string          `json:"tipo"`
	Cor       string          `json:"cor"`
	Texto     string          `json:"texto"`
	Palavras  json.RawMessage `json:"palavras"`
	Version   int64           `json:"version"`
	UpdatedAt time.Time       `json:"updated_at"`
	DeletedAt *time.Time      `json:"deleted_at,omitempty"`
	Deleted   bool            `json:"deleted"`
}

func (s *Store) GetMarcacaoSyncByIDTx(tx *sql.Tx, id, usuarioID int64) (*MarcacaoSync, error) {
	var m MarcacaoSync
	var palavras []byte
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT id, artigo_id, usuario_id, COALESCE(client_id,''), pagina, tipo, cor, texto, palavras_json, version, updated_at, deleted_at FROM marcacoes WHERE id = $1 AND usuario_id = $2`, id, usuarioID).
		Scan(&m.ID, &m.ArtigoID, &m.UsuarioID, &m.ClientID, &m.Pagina, &m.Tipo, &m.Cor, &m.Texto, &palavras, &m.Version, &m.UpdatedAt, &deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Palavras = json.RawMessage(palavras)
	if deletedAt.Valid {
		m.DeletedAt = &deletedAt.Time
		m.Deleted = true
	}
	return &m, nil
}

func (s *Store) GetMarcacaoSyncByClientIDTx(tx *sql.Tx, clientID string, usuarioID int64) (*MarcacaoSync, error) {
	var m MarcacaoSync
	var palavras []byte
	var deletedAt sql.NullTime
	err := tx.QueryRow(`SELECT id, artigo_id, usuario_id, COALESCE(client_id,''), pagina, tipo, cor, texto, palavras_json, version, updated_at, deleted_at FROM marcacoes WHERE client_id = $1 AND usuario_id = $2`, clientID, usuarioID).
		Scan(&m.ID, &m.ArtigoID, &m.UsuarioID, &m.ClientID, &m.Pagina, &m.Tipo, &m.Cor, &m.Texto, &palavras, &m.Version, &m.UpdatedAt, &deletedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	m.Palavras = json.RawMessage(palavras)
	if deletedAt.Valid {
		m.DeletedAt = &deletedAt.Time
		m.Deleted = true
	}
	return &m, nil
}

func (s *Store) InsertMarcacaoSyncTx(tx *sql.Tx, usuarioID, artigoID int64, clientID string, pagina int, tipo, cor string, palavrasJSON []byte, texto string) (*MarcacaoSync, error) {
	var id int64
	var version int64
	var updatedAt time.Time
	if palavrasJSON == nil {
		palavrasJSON = []byte("[]")
	}
	err := tx.QueryRow(`INSERT INTO marcacoes (artigo_id, usuario_id, client_id, pagina, tipo, cor, palavras_json, texto, version, updated_at) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,1,now()) RETURNING id, version, updated_at`,
		artigoID, usuarioID, clientID, pagina, tipo, cor, string(palavrasJSON), texto).Scan(&id, &version, &updatedAt)
	if err != nil {
		return nil, err
	}
	return &MarcacaoSync{ID: id, ArtigoID: artigoID, UsuarioID: usuarioID, ClientID: clientID, Pagina: pagina, Tipo: tipo, Cor: cor, Texto: texto, Palavras: json.RawMessage(palavrasJSON), Version: version, UpdatedAt: updatedAt}, nil
}

func (s *Store) UpdateMarcacaoSyncTx(tx *sql.Tx, id, usuarioID int64, cor *string, texto *string, palavrasJSON *[]byte) (*MarcacaoSync, error) {
	set := []string{"version = version + 1", "updated_at = now()"}
	args := []any{}
	idx := 1
	if cor != nil {
		set = append(set, "cor = $"+strconv.Itoa(idx))
		args = append(args, *cor)
		idx++
	}
	if texto != nil {
		set = append(set, "texto = $"+strconv.Itoa(idx))
		args = append(args, *texto)
		idx++
	}
	if palavrasJSON != nil {
		set = append(set, "palavras_json = $"+strconv.Itoa(idx))
		args = append(args, string(*palavrasJSON))
		idx++
	}
	args = append(args, id, usuarioID)
	query := "UPDATE marcacoes SET " + strings.Join(set, ", ") + " WHERE id = $" + strconv.Itoa(idx) + " AND usuario_id = $" + strconv.Itoa(idx+1) + " AND deleted_at IS NULL RETURNING version, updated_at"
	var version int64
	var updatedAt time.Time
	if err := tx.QueryRow(query, args...).Scan(&version, &updatedAt); err != nil {
		return nil, err
	}
	return &MarcacaoSync{ID: id, Version: version, UpdatedAt: updatedAt}, nil
}

func (s *Store) SoftDeleteMarcacaoSyncTx(tx *sql.Tx, id, usuarioID int64) (int64, time.Time, error) {
	var version int64
	var updatedAt time.Time
	err := tx.QueryRow(`UPDATE marcacoes SET deleted_at = now(), updated_at = now(), version = version + 1 WHERE id = $1 AND usuario_id = $2 AND deleted_at IS NULL RETURNING version, updated_at`, id, usuarioID).Scan(&version, &updatedAt)
	if err == sql.ErrNoRows {
		return 0, time.Time{}, sql.ErrNoRows
	}
	return version, updatedAt, err
}

// Jobs

type Job struct {
	ID            int64           `json:"id"`
	Tipo          string          `json:"tipo"`
	Payload       json.RawMessage `json:"payload"`
	Status        string          `json:"status"`
	Attempts      int             `json:"attempts"`
	MaxAttempts   int             `json:"max_attempts"`
	NextAttemptAt time.Time       `json:"next_attempt_at"`
	CreatedAt     time.Time       `json:"created_at"`
	UpdatedAt     time.Time       `json:"updated_at"`
	Result        json.RawMessage `json:"result,omitempty"`
	ErrorText     *string         `json:"error,omitempty"`
	UsuarioID     int64           `json:"usuario_id"`
}

func (s *Store) EnqueueJob(usuarioID int64, tipo string, payload json.RawMessage) (Job, error) {
	if payload == nil {
		payload = json.RawMessage(`{}`)
	}
	var j Job
	var result []byte
	var errText sql.NullString
	var uid sql.NullInt64
	err := s.db.QueryRow(`INSERT INTO jobs (tipo, payload, status, attempts, max_attempts, next_attempt_at, usuario_id) VALUES ($1,$2,'pending',0,5,now(),$3) RETURNING id, tipo, payload, status, attempts, max_attempts, next_attempt_at, created_at, updated_at, result, error_text, usuario_id`,
		tipo, string(payload), usuarioID).Scan(&j.ID, &j.Tipo, &j.Payload, &j.Status, &j.Attempts, &j.MaxAttempts, &j.NextAttemptAt, &j.CreatedAt, &j.UpdatedAt, &result, &errText, &uid)
	if err != nil {
		return Job{}, err
	}
	if result != nil {
		j.Result = json.RawMessage(result)
	}
	if errText.Valid {
		j.ErrorText = &errText.String
	}
	if uid.Valid {
		j.UsuarioID = uid.Int64
	}
	return j, nil
}

func (s *Store) GetJob(usuarioID, id int64) (Job, bool, error) {
	var j Job
	var result []byte
	var errText sql.NullString
	var payload []byte
	var uid sql.NullInt64
	err := s.db.QueryRow(`SELECT id, tipo, payload, status, attempts, max_attempts, next_attempt_at, created_at, updated_at, result, error_text, usuario_id FROM jobs WHERE id = $1 AND usuario_id = $2`, id, usuarioID).
		Scan(&j.ID, &j.Tipo, &payload, &j.Status, &j.Attempts, &j.MaxAttempts, &j.NextAttemptAt, &j.CreatedAt, &j.UpdatedAt, &result, &errText, &uid)
	if err == sql.ErrNoRows {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, err
	}
	j.Payload = json.RawMessage(payload)
	if result != nil {
		j.Result = json.RawMessage(result)
	}
	if errText.Valid {
		j.ErrorText = &errText.String
	}
	if uid.Valid {
		j.UsuarioID = uid.Int64
	}
	return j, true, nil
}

func (s *Store) ListJobs(usuarioID int64, limit int) ([]Job, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	rows, err := s.db.Query(`SELECT id, tipo, payload, status, attempts, max_attempts, next_attempt_at, created_at, updated_at, result, error_text, usuario_id FROM jobs WHERE usuario_id = $1 ORDER BY id DESC LIMIT $2`, usuarioID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		var j Job
		var payload, result []byte
		var errText sql.NullString
		var uid sql.NullInt64
		if err := rows.Scan(&j.ID, &j.Tipo, &payload, &j.Status, &j.Attempts, &j.MaxAttempts, &j.NextAttemptAt, &j.CreatedAt, &j.UpdatedAt, &result, &errText, &uid); err != nil {
			return nil, err
		}
		j.Payload = json.RawMessage(payload)
		if result != nil {
			j.Result = json.RawMessage(result)
		}
		if errText.Valid {
			j.ErrorText = &errText.String
		}
		if uid.Valid {
			j.UsuarioID = uid.Int64
		}
		out = append(out, j)
	}
	if out == nil {
		out = []Job{}
	}
	return out, rows.Err()
}

// ClaimNextJob usa SELECT ... FOR UPDATE SKIP LOCKED para concorrência segura entre workers.
func (s *Store) ClaimNextJob() (*Job, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var j Job
	var payload, result []byte
	var errText sql.NullString
	var uid sql.NullInt64
	err = tx.QueryRow(`SELECT id, tipo, payload, status, attempts, max_attempts, next_attempt_at, created_at, updated_at, result, error_text, usuario_id FROM jobs WHERE status IN ('pending','retry') AND next_attempt_at <= now() ORDER BY next_attempt_at, id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(&j.ID, &j.Tipo, &payload, &j.Status, &j.Attempts, &j.MaxAttempts, &j.NextAttemptAt, &j.CreatedAt, &j.UpdatedAt, &result, &errText, &uid)
	if err == sql.ErrNoRows {
		_ = tx.Commit()
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	j.Payload = json.RawMessage(payload)
	if result != nil {
		j.Result = json.RawMessage(result)
	}
	if errText.Valid {
		j.ErrorText = &errText.String
	}
	if uid.Valid {
		j.UsuarioID = uid.Int64
	}
	_, err = tx.Exec(`UPDATE jobs SET status = 'running', attempts = attempts + 1, updated_at = now() WHERE id = $1`, j.ID)
	if err != nil {
		return nil, err
	}
	j.Status = "running"
	j.Attempts++
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &j, nil
}

func (s *Store) CompleteJob(id int64, result json.RawMessage) error {
	if result == nil {
		result = json.RawMessage(`{}`)
	}
	_, err := s.db.Exec(`UPDATE jobs SET status = 'done', result = $2, updated_at = now() WHERE id = $1`, id, string(result))
	return err
}

func (s *Store) FailJob(id int64, errMsg string) error {
	// backoff exponencial: 2^attempts segundos, máx 5min
	_, err := s.db.Exec(`UPDATE jobs SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'retry' END, error_text = $2, next_attempt_at = now() + (LEAST(POWER(2, attempts)::int, 300) || ' seconds')::interval, updated_at = now() WHERE id = $1`, id, errMsg)
	return err
}

// --- Persistencia Neon: PDFs e PNGs em BYTEA (Render Free disco efemero) ---

func (s *Store) SetArtigoPdfData(id int64, data []byte) error {
	_, err := s.db.Exec(`UPDATE artigos SET pdf_data = $1 WHERE id = $2`, data, id)
	return err
}

func (s *Store) GetArtigoPdfData(id int64) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT pdf_data FROM artigos WHERE id = $1`, id).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return nil, false, nil
	}
	return data, true, nil
}

func (s *Store) SetPaginaImagemData(artigoID int64, numero int, data []byte) error {
	_, err := s.db.Exec(`UPDATE paginas SET imagem_data = $1 WHERE artigo_id = $2 AND numero = $3`, data, artigoID, numero)
	return err
}

func (s *Store) GetPaginaImagemData(artigoID int64, numero int) ([]byte, bool, error) {
	var data []byte
	err := s.db.QueryRow(`SELECT imagem_data FROM paginas WHERE artigo_id = $1 AND numero = $2`, artigoID, numero).Scan(&data)
	if err == sql.ErrNoRows {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if data == nil {
		return nil, false, nil
	}
	return data, true, nil
}

// HydrateFromDB restaura arquivos faltantes no disco a partir do BYTEA (best-effort, chamado no boot ou lazy).
func (s *Store) HydratePaginaImagem(artigoID int64, numero int, destPath string) (bool, error) {
	data, found, err := s.GetPaginaImagemData(artigoID, numero)
	if err != nil || !found || len(data) == 0 {
		return false, err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return false, err
	}
	if err := os.WriteFile(destPath, data, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// --- governanca: dispositivos/IPs autorizados ---

type DispositivoAutorizado struct {
	ID            int64     `json:"id"`
	Identificador string    `json:"identificador"`
	Tipo          string    `json:"tipo"`
	Descricao     string    `json:"descricao"`
	CriadoEm      time.Time `json:"criado_em"`
}

func (s *Store) EnsureDispositivoAutorizado(identificador, tipo, descricao string) error {
	identificador = strings.TrimSpace(identificador)
	tipo = strings.TrimSpace(tipo)
	if identificador == "" || tipo == "" {
		return fmt.Errorf("identificador e tipo obrigatórios")
	}
	_, err := s.db.Exec(`INSERT INTO dispositivos_autorizados (identificador, tipo, descricao) VALUES ($1,$2,$3) ON CONFLICT (identificador, tipo) DO UPDATE SET descricao = EXCLUDED.descricao`, identificador, tipo, descricao)
	return err
}

func (s *Store) IsDispositivoAutorizado(identificador, tipo string) (bool, error) {
	var exists bool
	err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM dispositivos_autorizados WHERE identificador = $1 AND tipo = $2)`, identificador, tipo).Scan(&exists)
	return exists, err
}

func (s *Store) IsIPAutorizado(ip string) (bool, error) {
	return s.IsDispositivoAutorizado(strings.TrimSpace(ip), "ip")
}

func (s *Store) ListDispositivosAutorizados() ([]DispositivoAutorizado, error) {
	rows, err := s.db.Query(`SELECT id, identificador, tipo, COALESCE(descricao,''), criado_em FROM dispositivos_autorizados ORDER BY criado_em DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DispositivoAutorizado
	for rows.Next() {
		var d DispositivoAutorizado
		if err := rows.Scan(&d.ID, &d.Identificador, &d.Tipo, &d.Descricao, &d.CriadoEm); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	if out == nil {
		out = []DispositivoAutorizado{}
	}
	return out, rows.Err()
}

func (s *Store) DeleteDispositivoAutorizado(id int64) (bool, error) {
	res, err := s.db.Exec(`DELETE FROM dispositivos_autorizados WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
