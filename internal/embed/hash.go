package embed

import (
	"hash/fnv"
	"strings"
	"unicode"
)

// Hash is a deterministic, dependency-free embedder: hashed bag of word
// unigrams, bigrams, and character trigrams projected into a fixed-size
// space (feature hashing with a sign trick).
//
// It is NOT semantic — paraphrases only match through shared tokens. It
// exists so tests and offline development have stable vectors, and as an
// explicit opt-in. It is never the silent default; that is the BM25-only
// floor.
type Hash struct{ dims int }

// NewHash returns a hash embedder with the given dimensionality.
func NewHash(dims int) *Hash { return &Hash{dims: dims} }

func (h *Hash) Name() string { return "hash-v1" }
func (h *Hash) Dims() int    { return h.dims }

func (h *Hash) Embed(text string) ([]float32, error) {
	v := make([]float32, h.dims)
	toks := tokenizeWords(text)
	add := func(feature string, weight float32) {
		f := fnv.New64a()
		f.Write([]byte(feature))
		sum := f.Sum64()
		idx := int(sum % uint64(h.dims))
		sign := float32(1)
		if (sum>>63)&1 == 1 {
			sign = -1
		}
		v[idx] += sign * weight
	}
	for i, t := range toks {
		add("u:"+t, 1.0)
		if i+1 < len(toks) {
			add("b:"+t+"_"+toks[i+1], 0.75)
		}
		for j := 0; j+3 <= len(t); j++ {
			add("c:"+t[j:j+3], 0.25)
		}
	}
	return Normalize(v), nil
}

func (h *Hash) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := h.Embed(t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

func tokenizeWords(s string) []string {
	s = strings.ToLower(s)
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r)
	})
}
