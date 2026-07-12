package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSkillMatchesRepoFile keeps the embedded skill and the repo copy in
// sync (the repo file is what ships in the awesome-list / docs; the
// embedded one is what `hooks install` writes).
func TestSkillMatchesRepoFile(t *testing.T) {
	repo, err := os.ReadFile(filepath.Join("..", "..", "integrations", "claude-code", "skill", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(repo)) != strings.TrimSpace(SkillMD) {
		t.Fatal("integrations/claude-code/skill/SKILL.md is out of sync with hooks.SkillMD")
	}
}

func TestBuildPlanIdempotent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMBER_CLAUDE_DIR", dir)
	settings := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(settings, []byte(`{"model":"opus"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan(false)
	if err != nil {
		t.Fatal(err)
	}
	if plan.AlreadyWired {
		t.Fatal("should not be wired yet")
	}
	if err := Apply(plan); err != nil {
		t.Fatal(err)
	}
	// Existing keys preserved.
	var root map[string]any
	raw, _ := os.ReadFile(settings)
	if err := json.Unmarshal(raw, &root); err != nil {
		t.Fatal(err)
	}
	if root["model"] != "opus" {
		t.Fatal("existing settings key clobbered")
	}
	if _, ok := root["hooks"]; !ok {
		t.Fatal("hooks not added")
	}
	// Second run is a no-op.
	plan2, err := BuildPlan(false)
	if err != nil {
		t.Fatal(err)
	}
	if !plan2.AlreadyWired {
		t.Fatal("second BuildPlan should detect existing amber hooks")
	}
	// A backup was written by the first Apply.
	entries, _ := filepath.Glob(settings + ".amber-backup.*")
	if len(entries) == 0 {
		t.Fatal("no backup written")
	}
}

func TestBuildPlanRefusesInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("AMBER_CLAUDE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := BuildPlan(false); err == nil {
		t.Fatal("expected refusal on invalid settings JSON")
	}
}
