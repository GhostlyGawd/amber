// Package embed provides embedding providers (decision D4).
//
// The default posture is keyless and offline: a local static
// Model2Vec-class model with pure-Go inference. API providers are opt-in.
// When no embedder is configured, Amber falls back to BM25-only recall —
// the "lexical floor". A store never mixes embedding models: the model
// identity is pinned in the meta table and `amber doctor --reembed`
// migrates (§6).
package embed

import (
	"fmt"
	"math"

	"github.com/ghostlygawd/amber/internal/config"
)

// Embedder turns text into a fixed-size vector.
type Embedder interface {
	// Name identifies the model (pinned in store meta).
	Name() string
	Dims() int
	Embed(text string) ([]float32, error)
	// EmbedBatch may be more efficient for providers with request overhead.
	EmbedBatch(texts []string) ([][]float32, error)
}

// ErrNone signals BM25-only mode (no embedder configured).
var ErrNone = fmt.Errorf("no embedding provider configured (BM25-only floor)")

// New constructs the configured embedder. provider "none" returns
// (nil, nil): callers treat a nil Embedder as the BM25-only floor.
func New(cfg *config.Config) (Embedder, error) {
	switch cfg.Embedding.Provider {
	case "", "none":
		return nil, nil
	case "hash":
		dims := cfg.Embedding.Dims
		if dims <= 0 {
			dims = 256
		}
		return NewHash(dims), nil
	case "local":
		dir, err := config.ModelsDir()
		if err != nil {
			return nil, err
		}
		model := cfg.Embedding.Model
		if model == "" {
			model = DefaultLocalModel
		}
		return LoadModel2Vec(dir, model)
	case "openai-compat":
		return NewOpenAICompat(cfg.Embedding)
	default:
		return nil, fmt.Errorf("unknown embedding provider %q", cfg.Embedding.Provider)
	}
}

// Cosine returns the cosine similarity of two vectors (0 when shapes
// mismatch). Inputs need not be pre-normalized.
func Cosine(a, b []float32) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	i := 0
	// 4-way unroll: this loop is the hot path of brute-force search.
	for ; i+3 < len(a); i += 4 {
		dot += float64(a[i])*float64(b[i]) + float64(a[i+1])*float64(b[i+1]) +
			float64(a[i+2])*float64(b[i+2]) + float64(a[i+3])*float64(b[i+3])
		na += float64(a[i])*float64(a[i]) + float64(a[i+1])*float64(a[i+1]) +
			float64(a[i+2])*float64(a[i+2]) + float64(a[i+3])*float64(a[i+3])
		nb += float64(b[i])*float64(b[i]) + float64(b[i+1])*float64(b[i+1]) +
			float64(b[i+2])*float64(b[i+2]) + float64(b[i+3])*float64(b[i+3])
	}
	for ; i < len(a); i++ {
		dot += float64(a[i]) * float64(b[i])
		na += float64(a[i]) * float64(a[i])
		nb += float64(b[i]) * float64(b[i])
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// Normalize scales v to unit length in place and returns it.
func Normalize(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return v
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
	return v
}
