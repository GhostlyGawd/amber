package embed

import (
	"encoding/binary"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// buildSyntheticModel writes a tiny valid safetensors + tokenizer.json so
// the loader and WordPiece path are tested without any network download.
func buildSyntheticModel(t *testing.T, dir string) {
	t.Helper()
	model := filepath.Join(dir, "synthetic")
	if err := os.MkdirAll(model, 0o755); err != nil {
		t.Fatal(err)
	}
	vocab := map[string]int{
		"[UNK]": 0, "the": 1, "deploy": 2, "##ment": 3, "pipeline": 4, "uses": 5, "postgres": 6,
	}
	dims := 4
	// Row i gets a distinct one-hot-ish pattern.
	data := make([]byte, len(vocab)*dims*4)
	for i := 0; i < len(vocab); i++ {
		for d := 0; d < dims; d++ {
			v := float32(0.1)
			if d == i%dims {
				v = 1.0
			}
			binary.LittleEndian.PutUint32(data[(i*dims+d)*4:], math.Float32bits(v))
		}
	}
	header := map[string]any{
		"embeddings": map[string]any{
			"dtype": "F32", "shape": []int{len(vocab), dims},
			"data_offsets": []int{0, len(data)},
		},
	}
	hb, _ := json.Marshal(header)
	st := make([]byte, 8+len(hb)+len(data))
	binary.LittleEndian.PutUint64(st, uint64(len(hb)))
	copy(st[8:], hb)
	copy(st[8+len(hb):], data)
	if err := os.WriteFile(filepath.Join(model, "model.safetensors"), st, 0o644); err != nil {
		t.Fatal(err)
	}
	tok := map[string]any{
		"normalizer": map[string]any{"type": "BertNormalizer", "lowercase": true},
		"model": map[string]any{
			"type": "WordPiece", "vocab": vocab, "unk_token": "[UNK]",
			"continuing_subword_prefix": "##",
		},
	}
	tb, _ := json.Marshal(tok)
	if err := os.WriteFile(filepath.Join(model, "tokenizer.json"), tb, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestModel2VecLoadAndEmbed(t *testing.T) {
	dir := t.TempDir()
	buildSyntheticModel(t, dir)
	m, err := LoadModel2Vec(dir, "synthetic")
	if err != nil {
		t.Fatal(err)
	}
	if m.Dims() != 4 {
		t.Fatalf("dims = %d", m.Dims())
	}
	// "Deployment" must split into deploy + ##ment (WordPiece), and the
	// vector must be the normalized mean of those rows.
	v1, err := m.Embed("The deployment pipeline uses Postgres")
	if err != nil {
		t.Fatal(err)
	}
	if vecNorm(v1) < 0.99 || vecNorm(v1) > 1.01 {
		t.Fatalf("embedding not normalized: |v| = %f", vecNorm(v1))
	}
	v2, _ := m.Embed("deployment pipeline")
	v3, _ := m.Embed("postgres")
	simNear := Cosine(v1, v2)
	simFar := Cosine(v2, v3)
	if simNear <= simFar {
		t.Fatalf("overlapping text should be closer: near %.3f, far %.3f", simNear, simFar)
	}
	// Unknown words fall to [UNK], never crash.
	if _, err := m.Embed("zzzqqq unknownword"); err != nil {
		t.Fatal(err)
	}
	// Empty input yields a zero vector, not an error.
	vz, err := m.Embed("")
	if err != nil || vecNorm(vz) != 0 {
		t.Fatalf("empty embed: %v |v|=%f", err, vecNorm(vz))
	}
}

func TestHashEmbedderDeterministic(t *testing.T) {
	h := NewHash(256)
	a1, _ := h.Embed("User prefers pytest over unittest")
	a2, _ := h.Embed("User prefers pytest over unittest")
	if Cosine(a1, a2) < 0.9999 {
		t.Fatal("hash embedder not deterministic")
	}
	b, _ := h.Embed("The staging database is Postgres 16")
	if Cosine(a1, b) > 0.5 {
		t.Fatalf("unrelated texts too similar: %f", Cosine(a1, b))
	}
}

func vecNorm(v []float32) float64 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	return math.Sqrt(n)
}
