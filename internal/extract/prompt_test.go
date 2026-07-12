package extract

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPromptMatchesDoc enforces the radical-transparency invariant: the
// published prompt (docs/prompts/extract.md) must contain the canonical
// prompt string verbatim. If you edit the prompt, update the doc.
func TestPromptMatchesDoc(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "prompts", "extract.md"))
	if err != nil {
		t.Fatal(err)
	}
	prompt := strings.TrimSpace(Prompt())
	// The doc wraps the prompt in a ```text fence; strip nothing, just
	// require the exact block to appear.
	if !strings.Contains(string(doc), prompt) {
		t.Fatal("docs/prompts/extract.md is out of sync with internal/extract/prompt.go — update the doc")
	}
}
