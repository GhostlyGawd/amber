// Command longmemeval runs Amber against LongMemEval-S (decision D11).
//
// Benchmark posture is radical transparency: this harness, the judge
// prompt (eval/prompts/judge.md), seeds, and per-question results —
// including losses — are published together. Reproduce with:
//
//	go run ./eval/longmemeval \
//	  -dataset longmemeval_s.json \
//	  -answer-cmd  'claude -p'   # any command: prompt on stdin, answer on stdout
//	  -judge-cmd   'claude -p'
//
// The dataset is not vendored (license); fetch it from the LongMemEval
// repository. Nothing here requires network — both LLM roles are
// arbitrary commands you control.
package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/config"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
	"github.com/ghostlygawd/amber/internal/trust"
	"github.com/ghostlygawd/amber/internal/writer"
)

// question mirrors the LongMemEval-S record shape (defensively parsed).
type question struct {
	QuestionID   string `json:"question_id"`
	QuestionType string `json:"question_type"`
	Question     string `json:"question"`
	Answer       any    `json:"answer"`
	QuestionDate string `json:"question_date"`
	Haystack     []struct {
		Sessions [][]struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"sessions"`
	} `json:"-"`
	HaystackSessions [][]struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"haystack_sessions"`
	HaystackDates []string `json:"haystack_dates"`
}

type result struct {
	QuestionID string  `json:"question_id"`
	Type       string  `json:"question_type"`
	Question   string  `json:"question"`
	Expected   any     `json:"expected"`
	Got        string  `json:"got"`
	Correct    bool    `json:"correct"`
	RecallMS   float64 `json:"recall_ms"`
	Retrieved  int     `json:"retrieved"`
}

func main() {
	dataset := flag.String("dataset", "", "path to longmemeval_s.json")
	answerCmd := flag.String("answer-cmd", "", "command answering questions (prompt on stdin)")
	judgeCmd := flag.String("judge-cmd", "", "command judging answers (prompt on stdin)")
	limitQ := flag.Int("limit", 0, "limit questions (0 = all)")
	topK := flag.Int("k", 8, "memories retrieved per question")
	out := flag.String("out", "longmemeval-results.json", "results file (JSON, per-question)")
	seed := flag.String("seed", "amber-longmemeval-v1", "run identifier recorded in results")
	flag.Parse()
	if *dataset == "" || *answerCmd == "" || *judgeCmd == "" {
		fmt.Fprintln(os.Stderr, "usage: longmemeval -dataset FILE -answer-cmd CMD -judge-cmd CMD")
		os.Exit(2)
	}

	raw, err := os.ReadFile(*dataset)
	must(err)
	var questions []question
	must(json.Unmarshal(raw, &questions))
	if *limitQ > 0 && len(questions) > *limitQ {
		questions = questions[:*limitQ]
	}

	judgePromptTmpl, err := os.ReadFile("eval/prompts/judge.md")
	must(err)

	var results []result
	for i, q := range questions {
		r := runOne(q, *answerCmd, *judgeCmd, string(judgePromptTmpl), *topK)
		results = append(results, r)
		fmt.Printf("[%d/%d] %-24s %-22s correct=%v\n", i+1, len(questions), r.QuestionID, r.Type, r.Correct)
	}

	report(results, *out, *seed)
}

func runOne(q question, answerCmd, judgeCmd, judgeTmpl string, topK int) result {
	// Fresh store per question — LongMemEval haystacks are independent.
	dir, err := os.MkdirTemp("", "lme-*")
	must(err)
	defer os.RemoveAll(dir)
	s, err := store.Create(dir)
	must(err)
	defer s.Close()
	w := &writer.Writer{Store: s, Config: config.Default(), Embedder: embed.NewHash(256)}

	// Ingest haystack sessions as T2 memories, turn by turn. This is the
	// no-LLM ingestion floor: raw turns as memories. (A digest-based
	// variant costs one LLM call per session; run it by pointing
	// -answer-cmd style extraction at the store first.)
	for si, sess := range q.HaystackSessions {
		date := ""
		if si < len(q.HaystackDates) {
			date = q.HaystackDates[si]
		}
		for _, turn := range sess {
			content := strings.TrimSpace(turn.Content)
			if content == "" || len(content) > 3800 {
				if len(content) > 3800 {
					content = content[:3800]
				} else {
					continue
				}
			}
			prefix := ""
			if date != "" {
				prefix = "(" + date + ") "
			}
			_, _ = w.Write(writer.Input{
				Content: prefix + turn.Role + ": " + content,
				Type:    "note", Trust: trust.T2, Source: "longmemeval", SkipScan: true,
			})
		}
	}

	t0 := time.Now()
	rs, err := search.Recall(s, w.Embedder, search.Request{Query: q.Question, Limit: topK})
	must(err)
	recallMS := float64(time.Since(t0).Microseconds()) / 1000

	var ctxB strings.Builder
	for _, r := range rs {
		fmt.Fprintf(&ctxB, "- %s\n", r.Memory.Content)
	}
	answerPrompt := fmt.Sprintf(
		"Answer the question using ONLY the retrieved memories below. If they do not contain the answer, say \"I don't know\".\n\nMemories:\n%s\nQuestion (asked %s): %s\nAnswer concisely.",
		ctxB.String(), q.QuestionDate, q.Question)
	got, err := runCmd(answerCmd, answerPrompt)
	if err != nil {
		got = "ERROR: " + err.Error()
	}

	judgePrompt := strings.NewReplacer(
		"{{QUESTION}}", q.Question,
		"{{EXPECTED}}", fmt.Sprint(q.Answer),
		"{{ANSWER}}", got,
	).Replace(judgeTmpl)
	verdict, err := runCmd(judgeCmd, judgePrompt)
	correct := err == nil && strings.Contains(strings.ToLower(firstLine(verdict)), "correct") &&
		!strings.Contains(strings.ToLower(firstLine(verdict)), "incorrect")

	return result{
		QuestionID: q.QuestionID, Type: q.QuestionType, Question: q.Question,
		Expected: q.Answer, Got: strings.TrimSpace(got), Correct: correct,
		RecallMS: recallMS, Retrieved: len(rs),
	}
}

func report(results []result, out, seed string) {
	byType := map[string][2]int{}
	correct := 0
	var recallTimes []float64
	for _, r := range results {
		c := byType[r.Type]
		c[1]++
		if r.Correct {
			c[0]++
			correct++
		}
		byType[r.Type] = c
		recallTimes = append(recallTimes, r.RecallMS)
	}
	sort.Float64s(recallTimes)
	payload := map[string]any{
		"seed": seed, "run_at": time.Now().UTC().Format(time.RFC3339),
		"total": len(results), "correct": correct,
		"accuracy":      float64(correct) / float64(max(1, len(results))),
		"by_type":       byType,
		"recall_p50_ms": pct(recallTimes, 0.5), "recall_p95_ms": pct(recallTimes, 0.95),
		"results": results, // full per-question detail, losses included
	}
	b, _ := json.MarshalIndent(payload, "", "  ")
	must(os.WriteFile(out, b, 0o644))
	fmt.Printf("\naccuracy %d/%d (%.1f%%) — full results incl. losses in %s\n",
		correct, len(results), 100*float64(correct)/float64(max(1, len(results))), out)
	for t, c := range byType {
		fmt.Printf("  %-28s %d/%d\n", t, c[0], c[1])
	}
}

func runCmd(command, stdin string) (string, error) {
	cmd := exec.Command("sh", "-c", command)
	cmd.Stdin = strings.NewReader(stdin)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("%v: %s", err, errb.String())
	}
	return out.String(), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i > 0 {
		return s[:i]
	}
	return s
}

func pct(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	i := int(p * float64(len(sorted)-1))
	return sorted[i]
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "longmemeval:", err)
		os.Exit(1)
	}
}
