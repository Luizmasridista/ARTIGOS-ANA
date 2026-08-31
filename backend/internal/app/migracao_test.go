package app

import (
	"database/sql"
	"encoding/json"
	"testing"
)

func TestMigracaoCamadasLegadas(t *testing.T) {
	_, a := newTestServer(t)

	db, err := sql.Open("pgx", testDSN)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	var artigoID int64
	if err := db.QueryRow(`INSERT INTO artigos (titulo, arquivo_pdf, criado_em) VALUES ('Legado', 'pdfs/x.pdf', now()) RETURNING id`).Scan(&artigoID); err != nil {
		t.Fatal(err)
	}

	camadaPontos := `[{"texto":"Olá","x0":70,"y0":87.7,"x1":90.2,"y1":102.2}]`
	if _, err := db.Exec(`INSERT INTO paginas (artigo_id, numero, imagem_png, camada_json, largura_px, altura_px) VALUES ($1, 1, 'paginas/x/1.png', $2, 1240, 1754)`, artigoID, camadaPontos); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO marcacoes (artigo_id, pagina, tipo, cor, palavras_json, texto) VALUES ($1, 1, 'highlight', '#FFEB3B', '[[70,87.7,90.2,102.2]]', 'legado')`, artigoID); err != nil {
		t.Fatal(err)
	}

	if err := a.DB.MigrateCamadasLegadas(); err != nil {
		t.Fatalf("migração: %v", err)
	}

	const fator = 150.0 / 72.0

	var camadaRaw []byte
	if err := db.QueryRow(`SELECT camada_json FROM paginas WHERE artigo_id = $1`, artigoID).Scan(&camadaRaw); err != nil {
		t.Fatal(err)
	}
	var palavras []struct {
		Texto string  `json:"texto"`
		X0    float64 `json:"x0"`
		Y0    float64 `json:"y0"`
	}
	if err := json.Unmarshal(camadaRaw, &palavras); err != nil || len(palavras) != 1 {
		t.Fatalf("camada migrada ilegível: %s", camadaRaw)
	}
	if got, want := palavras[0].X0, 70*fator; got < want-0.5 || got > want+0.5 {
		t.Errorf("X0 da camada não migrado: got %v, want %v", got, want)
	}
	if got, want := palavras[0].Y0, 87.7*fator; got < want-0.5 || got > want+0.5 {
		t.Errorf("Y0 da camada não migrado: got %v, want %v", got, want)
	}

	var marcRaw []byte
	if err := db.QueryRow(`SELECT palavras_json FROM marcacoes WHERE artigo_id = $1`, artigoID).Scan(&marcRaw); err != nil {
		t.Fatal(err)
	}
	var caixas [][]float64
	if err := json.Unmarshal(marcRaw, &caixas); err != nil || len(caixas) != 1 || len(caixas[0]) != 4 {
		t.Fatalf("marcação migrada ilegível: %s", marcRaw)
	}
	if got, want := caixas[0][0], 70*fator; got < want-0.5 || got > want+0.5 {
		t.Errorf("marcação não migrada: got %v, want %v", got, want)
	}
}
