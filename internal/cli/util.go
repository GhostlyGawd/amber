package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
)

func newJSONEncoder(w io.Writer) *json.Encoder {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc
}

// shortID renders the display prefix of an id.
func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}

// ago renders a compact relative time ("3d", "2h", "now").
func ago(t time.Time) string {
	if t.IsZero() {
		return "-"
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 60*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return t.Format("2006-01-02")
	}
}

// memLine renders one memory as a single list row.
func memLine(m *store.Memory) string {
	badge := ""
	switch m.Status {
	case store.StatusQuarantined:
		badge = " [quarantined]"
	case store.StatusSuperseded:
		badge = " [superseded]"
	case store.StatusTombstoned:
		badge = " [tombstoned]"
	case store.StatusAging:
		badge = " [aging]"
	}
	return fmt.Sprintf("%s  %-10s %s  %s%s", shortID(m.ID), m.Type, m.Trust.String(), oneLine(m.Content, 100), badge)
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-1] + "…"
	}
	return s
}

// printResults renders recall hits in text form, optionally with
// attribution (--why).
func printResults(w io.Writer, results []search.Result, why bool) {
	if len(results) == 0 {
		fmt.Fprintln(w, "no memories matched")
		return
	}
	for i, r := range results {
		m := r.Memory
		fmt.Fprintf(w, "%2d. %s\n", i+1, memLine(m))
		if len(m.Entities) > 0 {
			names := make([]string, len(m.Entities))
			for j, e := range m.Entities {
				names[j] = e.Name
			}
			fmt.Fprintf(w, "    entities: %s\n", strings.Join(names, ", "))
		}
		if why {
			fmt.Fprintf(w, "    why: %s\n", search.FormatWhy(r))
		}
	}
}

// resultJSON is the stable JSON shape for recall output.
type resultJSON struct {
	ID         string   `json:"id"`
	Content    string   `json:"content"`
	Type       string   `json:"type"`
	Trust      string   `json:"trust"`
	Status     string   `json:"status"`
	Importance int      `json:"importance"`
	Score      float64  `json:"score"`
	Entities   []string `json:"entities,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	UpdatedAt  string   `json:"updated_at"`
	Why        any      `json:"why,omitempty"`
}

func toResultJSON(rs []search.Result, why bool) []resultJSON {
	out := make([]resultJSON, 0, len(rs))
	for _, r := range rs {
		m := r.Memory
		var names []string
		for _, e := range m.Entities {
			names = append(names, e.Name)
		}
		rj := resultJSON{
			ID: m.ID, Content: m.Content, Type: m.Type, Trust: m.Trust.String(),
			Status: m.Status, Importance: m.Importance, Score: r.Score,
			Entities: names, Tags: m.Tags, UpdatedAt: m.UpdatedAt.Format(time.RFC3339),
		}
		if why {
			rj.Why = r.Why
		}
		out = append(out, rj)
	}
	return out
}
