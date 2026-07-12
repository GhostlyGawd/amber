package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ghostlygawd/amber/internal/config"
)

// OpenAICompat calls any OpenAI-compatible /v1/embeddings endpoint
// (OpenAI, Voyage, Ollama, LM Studio, vLLM…). Strictly opt-in: it is never
// selected automatically, and the API key lives only in the environment
// variable named by embedding.api_key_env — never in config or the store.
type OpenAICompat struct {
	endpoint string
	model    string
	keyEnv   string
	dims     int
	client   *http.Client
}

// NewOpenAICompat validates configuration and returns the provider.
func NewOpenAICompat(ec config.Embedding) (*OpenAICompat, error) {
	if ec.Endpoint == "" {
		return nil, fmt.Errorf("embedding.endpoint required for openai-compat provider")
	}
	if ec.Model == "" {
		return nil, fmt.Errorf("embedding.model required for openai-compat provider")
	}
	return &OpenAICompat{
		endpoint: ec.Endpoint,
		model:    ec.Model,
		keyEnv:   ec.APIKeyEnv,
		dims:     ec.Dims,
		client:   &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (o *OpenAICompat) Name() string { return "openai-compat/" + o.model }
func (o *OpenAICompat) Dims() int    { return o.dims }

func (o *OpenAICompat) Embed(text string) ([]float32, error) {
	vs, err := o.EmbedBatch([]string{text})
	if err != nil {
		return nil, err
	}
	return vs[0], nil
}

func (o *OpenAICompat) EmbedBatch(texts []string) ([][]float32, error) {
	body, _ := json.Marshal(map[string]any{"model": o.model, "input": texts})
	req, err := http.NewRequest("POST", o.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if o.keyEnv != "" {
		if k := os.Getenv(o.keyEnv); k != "" {
			req.Header.Set("Authorization", "Bearer "+k)
		}
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 2048))
		return nil, fmt.Errorf("embeddings endpoint HTTP %d: %s", resp.StatusCode, string(b))
	}
	var out struct {
		Data []struct {
			Index     int       `json:"index"`
			Embedding []float32 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if len(out.Data) != len(texts) {
		return nil, fmt.Errorf("embeddings endpoint returned %d vectors for %d inputs", len(out.Data), len(texts))
	}
	vecs := make([][]float32, len(texts))
	for _, d := range out.Data {
		if d.Index < 0 || d.Index >= len(vecs) {
			return nil, fmt.Errorf("embeddings endpoint returned bad index %d", d.Index)
		}
		vecs[d.Index] = Normalize(d.Embedding)
	}
	if o.dims == 0 && len(vecs) > 0 {
		o.dims = len(vecs[0])
	}
	return vecs, nil
}
