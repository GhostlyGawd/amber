package embed

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// FetchModel downloads the local model files into modelsDir/name/.
// This is the ONLY network operation in Amber's default configuration,
// runs once at init with the user's consent, and everything is offline
// afterwards.
func FetchModel(modelsDir, name, baseURL string, progress io.Writer) error {
	if baseURL == "" {
		if name != DefaultLocalModel {
			return fmt.Errorf("no model_url configured for model %q", name)
		}
		baseURL = DefaultModelBaseURL
	}
	dir := filepath.Join(modelsDir, name)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	for _, f := range []string{"model.safetensors", "tokenizer.json", "config.json"} {
		dst := filepath.Join(dir, f)
		if fi, err := os.Stat(dst); err == nil && fi.Size() > 0 {
			continue // cached
		}
		url := baseURL + "/" + f
		if progress != nil {
			fmt.Fprintf(progress, "fetching %s\n", url)
		}
		if err := fetchOne(client, url, dst); err != nil {
			if f == "config.json" {
				continue // optional
			}
			return fmt.Errorf("fetch %s: %w", url, err)
		}
	}
	return nil
}

func fetchOne(client *http.Client, url, dst string) error {
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	tmp := dst + ".part"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// ModelCached reports whether the named model is already on disk.
func ModelCached(modelsDir, name string) bool {
	fi, err := os.Stat(filepath.Join(modelsDir, name, "model.safetensors"))
	return err == nil && fi.Size() > 0
}
