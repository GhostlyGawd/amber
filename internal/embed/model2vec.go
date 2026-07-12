package embed

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// DefaultLocalModel is the Model2Vec-class static model fetched at init
// (~30MB on disk, MIT-licensed). Inference is pure Go: tokenize, look up
// static token vectors, mean-pool, normalize.
const DefaultLocalModel = "potion-base-8M"

// DefaultModelBaseURL is where model files are fetched from at init.
const DefaultModelBaseURL = "https://huggingface.co/minishlab/potion-base-8M/resolve/main"

// Model2Vec is a static-embedding model loaded from safetensors +
// tokenizer.json.
type Model2Vec struct {
	name      string
	dims      int
	vectors   []float32 // vocab*dims, row-major
	vocab     map[string]int
	contPfx   string // WordPiece continuation prefix ("##")
	unkID     int
	lowercase bool
	maxTokLen int
}

// LoadModel2Vec loads a cached model from modelsDir/name/.
func LoadModel2Vec(modelsDir, name string) (*Model2Vec, error) {
	dir := filepath.Join(modelsDir, name)
	stPath := filepath.Join(dir, "model.safetensors")
	tokPath := filepath.Join(dir, "tokenizer.json")
	if _, err := os.Stat(stPath); err != nil {
		return nil, fmt.Errorf("local model %q not present at %s (run `amber init` or `amber doctor --fetch-model`)", name, dir)
	}
	vectors, vocabSize, dims, err := loadSafetensorsEmbeddings(stPath)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", stPath, err)
	}
	m := &Model2Vec{name: name, dims: dims, vectors: vectors, contPfx: "##", unkID: -1, lowercase: true, maxTokLen: 100}
	if err := m.loadTokenizer(tokPath); err != nil {
		return nil, fmt.Errorf("load %s: %w", tokPath, err)
	}
	if len(m.vocab) != vocabSize {
		// Tolerated: some exports carry extra rows; lookups bound-check.
		if len(m.vocab) > vocabSize {
			return nil, fmt.Errorf("tokenizer vocab (%d) larger than embedding rows (%d)", len(m.vocab), vocabSize)
		}
	}
	return m, nil
}

func (m *Model2Vec) Name() string { return "model2vec/" + m.name }
func (m *Model2Vec) Dims() int    { return m.dims }

func (m *Model2Vec) Embed(text string) ([]float32, error) {
	ids := m.tokenize(text)
	v := make([]float32, m.dims)
	if len(ids) == 0 {
		return v, nil
	}
	for _, id := range ids {
		row := m.vectors[id*m.dims : (id+1)*m.dims]
		for i, x := range row {
			v[i] += x
		}
	}
	inv := float32(1) / float32(len(ids))
	for i := range v {
		v[i] *= inv
	}
	return Normalize(v), nil
}

func (m *Model2Vec) EmbedBatch(texts []string) ([][]float32, error) {
	out := make([][]float32, len(texts))
	for i, t := range texts {
		v, err := m.Embed(t)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// --- tokenizer (HF tokenizer.json subset: WordPiece; greedy fallback for
// BPE/Unigram vocabularies — differences average out under mean pooling) ---

type hfTokenizer struct {
	Normalizer *struct {
		Type        string `json:"type"`
		Lowercase   *bool  `json:"lowercase"`
		StripAccent *bool  `json:"strip_accents"`
	} `json:"normalizer"`
	Model struct {
		Type                    string          `json:"type"`
		Vocab                   json.RawMessage `json:"vocab"`
		UnkToken                string          `json:"unk_token"`
		ContinuingSubwordPrefix string          `json:"continuing_subword_prefix"`
	} `json:"model"`
}

func (m *Model2Vec) loadTokenizer(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var tk hfTokenizer
	if err := json.Unmarshal(b, &tk); err != nil {
		return err
	}
	m.vocab = map[string]int{}
	switch {
	case len(tk.Model.Vocab) > 0 && tk.Model.Vocab[0] == '{':
		// WordPiece/BPE: {"token": id}
		var v map[string]int
		if err := json.Unmarshal(tk.Model.Vocab, &v); err != nil {
			return fmt.Errorf("vocab: %w", err)
		}
		m.vocab = v
	case len(tk.Model.Vocab) > 0 && tk.Model.Vocab[0] == '[':
		// Unigram: [["token", score], ...] — index is the id
		var v [][]any
		if err := json.Unmarshal(tk.Model.Vocab, &v); err != nil {
			return fmt.Errorf("unigram vocab: %w", err)
		}
		for i, pair := range v {
			if len(pair) > 0 {
				if s, ok := pair[0].(string); ok {
					m.vocab[s] = i
				}
			}
		}
	default:
		return fmt.Errorf("unsupported tokenizer vocab format")
	}
	if tk.Model.ContinuingSubwordPrefix != "" {
		m.contPfx = tk.Model.ContinuingSubwordPrefix
	}
	if id, ok := m.vocab[tk.Model.UnkToken]; ok {
		m.unkID = id
	}
	if tk.Normalizer != nil && tk.Normalizer.Lowercase != nil {
		m.lowercase = *tk.Normalizer.Lowercase
	}
	return nil
}

// tokenize: normalize → split words/punct → WordPiece greedy longest match.
func (m *Model2Vec) tokenize(text string) []int {
	if m.lowercase {
		text = strings.ToLower(text)
	}
	text = stripMn(norm.NFD.String(text))
	var ids []int
	for _, word := range splitWords(text) {
		if len(word) > m.maxTokLen {
			continue
		}
		ids = append(ids, m.wordpiece(word)...)
	}
	return ids
}

func (m *Model2Vec) wordpiece(word string) []int {
	var out []int
	runes := []rune(word)
	start := 0
	for start < len(runes) {
		end := len(runes)
		found := -1
		for end > start {
			sub := string(runes[start:end])
			if start > 0 {
				sub = m.contPfx + sub
			}
			if id, ok := m.vocab[sub]; ok && id*m.dims < len(m.vectors) {
				found = id
				break
			}
			end--
		}
		if found < 0 {
			if m.unkID >= 0 && m.unkID*m.dims < len(m.vectors) {
				out = append(out, m.unkID)
			}
			return out
		}
		out = append(out, found)
		start = end
	}
	return out
}

func splitWords(s string) []string {
	var words []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			words = append(words, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			flush()
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			flush()
			words = append(words, string(r))
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return words
}

func stripMn(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// --- safetensors (format: u64le header length, JSON header, raw data) ---

type stTensor struct {
	Dtype       string  `json:"dtype"`
	Shape       []int   `json:"shape"`
	DataOffsets []int64 `json:"data_offsets"`
}

func loadSafetensorsEmbeddings(path string) (vectors []float32, vocab, dims int, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, 0, 0, err
	}
	if len(b) < 8 {
		return nil, 0, 0, fmt.Errorf("truncated safetensors")
	}
	hlen := binary.LittleEndian.Uint64(b[:8])
	if hlen > uint64(len(b)-8) {
		return nil, 0, 0, fmt.Errorf("corrupt safetensors header")
	}
	var header map[string]json.RawMessage
	if err := json.Unmarshal(b[8:8+hlen], &header); err != nil {
		return nil, 0, 0, err
	}
	data := b[8+hlen:]
	// The embeddings tensor is usually named "embeddings"; fall back to the
	// first 2-D tensor.
	var t stTensor
	pick := func(raw json.RawMessage) bool {
		var cand stTensor
		if json.Unmarshal(raw, &cand) != nil {
			return false
		}
		if len(cand.Shape) == 2 {
			t = cand
			return true
		}
		return false
	}
	if raw, ok := header["embeddings"]; !ok || !pick(raw) {
		found := false
		for name, raw := range header {
			if name == "__metadata__" {
				continue
			}
			if pick(raw) {
				found = true
				break
			}
		}
		if !found {
			return nil, 0, 0, fmt.Errorf("no 2-D embeddings tensor in safetensors")
		}
	}
	vocab, dims = t.Shape[0], t.Shape[1]
	n := vocab * dims
	if len(t.DataOffsets) != 2 || t.DataOffsets[1] > int64(len(data)) || t.DataOffsets[0] >= t.DataOffsets[1] {
		return nil, 0, 0, fmt.Errorf("bad tensor offsets")
	}
	raw := data[t.DataOffsets[0]:t.DataOffsets[1]]
	vectors = make([]float32, n)
	switch t.Dtype {
	case "F32":
		if len(raw) < n*4 {
			return nil, 0, 0, fmt.Errorf("tensor data too short")
		}
		for i := 0; i < n; i++ {
			vectors[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[i*4:]))
		}
	case "F16":
		if len(raw) < n*2 {
			return nil, 0, 0, fmt.Errorf("tensor data too short")
		}
		for i := 0; i < n; i++ {
			vectors[i] = f16to32(binary.LittleEndian.Uint16(raw[i*2:]))
		}
	default:
		return nil, 0, 0, fmt.Errorf("unsupported tensor dtype %s (want F32 or F16)", t.Dtype)
	}
	return vectors, vocab, dims, nil
}

func f16to32(u uint16) float32 {
	sign := uint32(u>>15) << 31
	exp := uint32(u>>10) & 0x1f
	man := uint32(u) & 0x3ff
	switch exp {
	case 0:
		if man == 0 {
			return math.Float32frombits(sign)
		}
		// subnormal
		for man&0x400 == 0 {
			man <<= 1
			exp--
		}
		exp++
		man &= 0x3ff
		return math.Float32frombits(sign | (exp+112)<<23 | man<<13)
	case 0x1f:
		return math.Float32frombits(sign | 0xff<<23 | man<<13)
	default:
		return math.Float32frombits(sign | (exp+112)<<23 | man<<13)
	}
}
