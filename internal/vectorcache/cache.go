// Package vectorcache accelerates brute-force cosine search (§7) with a
// sidecar file of pre-normalized active/aging vectors.
//
// The database stays the source of truth (D2/D3: cosine over BLOB
// embeddings); the cache is a disposable derivation, keyed to the ops
// journal epoch — the append-only journal (D14) makes staleness
// detection a single MAX() comparison. Deleting the file is always safe.
//
// Layout (little-endian): magic, u32 header length, JSON header, id
// table, zero padding to 4-byte alignment, then count×dims float32. The
// alignment allows a zero-copy []float32 view of the vector region on
// little-endian hosts; big-endian hosts decode per float.
package vectorcache

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"unsafe"

	"github.com/ghostlygawd/amber/internal/store"
)

const magic = "AMBRVEC1"

// Cache holds pre-normalized vectors for dot-product scoring.
type Cache struct {
	IDs  []string
	Vecs []float32 // len = count*dims, rows L2-normalized
	Dims int

	epoch int64
	raw   []byte // pins the zero-copy backing array, when used
}

type header struct {
	Epoch int64  `json:"epoch"`
	Model string `json:"model"`
	Dims  int    `json:"dims"`
	Count int    `json:"count"`
}

// memo keeps parsed caches alive inside long-running processes (serve,
// browse, eval loops). Keyed by path; validated by epoch on every Open.
var memo sync.Map // path -> *Cache

// Path of the cache file inside a store directory.
func Path(storeDir string) string { return filepath.Join(storeDir, "vectors.cache") }

// Open returns a current cache, rebuilding it if missing or stale.
// Model name pins the embedding space; epoch pins store state.
func Open(s *store.Store, storeDir, model string, dims int) (*Cache, error) {
	epoch, err := s.VectorEpoch()
	if err != nil {
		return nil, err
	}
	path := Path(storeDir)
	if v, ok := memo.Load(path); ok {
		c := v.(*Cache)
		if c.epoch == epoch && c.Dims == dims {
			return c, nil
		}
	}
	if c, ok := load(path, model, dims, epoch); ok {
		memo.Store(path, c)
		return c, nil
	}
	c, err := build(s, path, model, dims, epoch)
	if err != nil {
		return nil, err
	}
	memo.Store(path, c)
	return c, nil
}

var littleEndian = func() bool {
	var x uint16 = 1
	return *(*byte)(unsafe.Pointer(&x)) == 1
}()

func load(path, model string, dims int, epoch int64) (*Cache, bool) {
	raw, err := os.ReadFile(path)
	if err != nil || len(raw) < len(magic)+4 {
		return nil, false
	}
	if string(raw[:len(magic)]) != magic {
		return nil, false
	}
	off := len(magic)
	hlen := int(binary.LittleEndian.Uint32(raw[off:]))
	off += 4
	if hlen <= 0 || off+hlen > len(raw) {
		return nil, false
	}
	var h header
	if json.Unmarshal(raw[off:off+hlen], &h) != nil {
		return nil, false
	}
	off += hlen
	if h.Epoch != epoch || h.Model != model || h.Dims != dims || h.Count < 0 {
		return nil, false
	}
	ids := make([]string, h.Count)
	for i := 0; i < h.Count; i++ {
		if off >= len(raw) {
			return nil, false
		}
		l := int(raw[off])
		off++
		if off+l > len(raw) {
			return nil, false
		}
		ids[i] = string(raw[off : off+l])
		off += l
	}
	off = align4(off)
	need := h.Count * h.Dims * 4
	if len(raw)-off < need {
		return nil, false
	}
	c := &Cache{IDs: ids, Dims: h.Dims, epoch: epoch}
	if littleEndian && need > 0 && uintptr(unsafe.Pointer(&raw[off]))%4 == 0 {
		// Zero-copy view over the file bytes.
		c.Vecs = unsafe.Slice((*float32)(unsafe.Pointer(&raw[off])), h.Count*h.Dims)
		c.raw = raw
	} else {
		vecs := make([]float32, h.Count*h.Dims)
		for i := range vecs {
			vecs[i] = math.Float32frombits(binary.LittleEndian.Uint32(raw[off+i*4:]))
		}
		c.Vecs = vecs
	}
	return c, true
}

func build(s *store.Store, path, model string, dims int, epoch int64) (*Cache, error) {
	c := &Cache{Dims: dims, epoch: epoch}
	err := s.ScanVectors([]string{store.StatusActive, store.StatusAging}, func(id string, vec []float32) error {
		if len(vec) != dims {
			return nil // foreign-dimension rows are skipped, not fatal
		}
		normalize(vec)
		c.IDs = append(c.IDs, id)
		c.Vecs = append(c.Vecs, vec...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	// Persist best-effort: a read-only store dir still gets in-memory speed.
	h, _ := json.Marshal(header{Epoch: epoch, Model: model, Dims: dims, Count: len(c.IDs)})
	idBytes := 0
	for _, id := range c.IDs {
		idBytes += 1 + len(id)
	}
	prefix := len(magic) + 4 + len(h) + idBytes
	total := align4(prefix) + len(c.Vecs)*4
	buf := make([]byte, 0, total)
	buf = append(buf, magic...)
	var hlen [4]byte
	binary.LittleEndian.PutUint32(hlen[:], uint32(len(h)))
	buf = append(buf, hlen[:]...)
	buf = append(buf, h...)
	for _, id := range c.IDs {
		if len(id) > 255 {
			return nil, fmt.Errorf("id too long for cache: %s", id)
		}
		buf = append(buf, byte(len(id)))
		buf = append(buf, id...)
	}
	for len(buf) < align4(len(buf)) {
		buf = append(buf, 0)
	}
	var w [4]byte
	for _, f := range c.Vecs {
		binary.LittleEndian.PutUint32(w[:], math.Float32bits(f))
		buf = append(buf, w[:]...)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err == nil {
		_ = os.Rename(tmp, path)
	}
	return c, nil
}

func align4(n int) int { return (n + 3) &^ 3 }

// Hit is one scored candidate.
type Hit struct {
	ID    string
	Score float64
}

// TopK scores q against every cached vector by dot product (rows are
// pre-normalized; q is normalized on a copy), optionally restricted to
// allowed ids, returning the k best descending.
func (c *Cache) TopK(q []float32, k int, allowed map[string]bool) []Hit {
	if len(q) != c.Dims || c.Dims == 0 || len(c.IDs) == 0 {
		return nil
	}
	qn := make([]float32, len(q))
	copy(qn, q)
	normalize(qn)
	hits := make([]Hit, 0, min(len(c.IDs), 4*k))
	worst := -1.0
	for i, id := range c.IDs {
		if allowed != nil && !allowed[id] {
			continue
		}
		row := c.Vecs[i*c.Dims : (i+1)*c.Dims]
		var dot float64
		j := 0
		for ; j+3 < len(row); j += 4 {
			dot += float64(qn[j])*float64(row[j]) + float64(qn[j+1])*float64(row[j+1]) +
				float64(qn[j+2])*float64(row[j+2]) + float64(qn[j+3])*float64(row[j+3])
		}
		for ; j < len(row); j++ {
			dot += float64(qn[j]) * float64(row[j])
		}
		if dot <= 0 {
			continue
		}
		if len(hits) >= 4*k && dot <= worst {
			continue
		}
		hits = append(hits, Hit{ID: id, Score: dot})
		if len(hits) >= 8*k {
			sort.Slice(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
			hits = hits[:4*k]
			worst = hits[len(hits)-1].Score
		}
	}
	sort.Slice(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if len(hits) > k {
		hits = hits[:k]
	}
	return hits
}

func normalize(v []float32) {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	if n == 0 {
		return
	}
	inv := float32(1 / math.Sqrt(n))
	for i := range v {
		v[i] *= inv
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
