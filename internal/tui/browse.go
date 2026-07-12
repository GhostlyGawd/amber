// Package tui implements `amber browse` (decision D17): a terminal-native
// inspector for the store — search, list, filter, and inspect memories;
// view supersedence chains and trust tiers.
//
// Visual identity per §30: amber-on-cream editorial warmth, not
// neon-green-on-black.
package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/ghostlygawd/amber/internal/belief"
	"github.com/ghostlygawd/amber/internal/embed"
	"github.com/ghostlygawd/amber/internal/search"
	"github.com/ghostlygawd/amber/internal/store"
)

var (
	amberCol   = lipgloss.AdaptiveColor{Light: "#B45309", Dark: "#F59E0B"}
	creamCol   = lipgloss.AdaptiveColor{Light: "#FFFBEB", Dark: "#1C1917"}
	inkCol     = lipgloss.AdaptiveColor{Light: "#292524", Dark: "#E7E5E4"}
	fadeCol    = lipgloss.AdaptiveColor{Light: "#78716C", Dark: "#A8A29E"}
	dangerCol  = lipgloss.AdaptiveColor{Light: "#B91C1C", Dark: "#F87171"}
	titleStyle = lipgloss.NewStyle().Foreground(amberCol).Bold(true)
	selStyle   = lipgloss.NewStyle().Foreground(creamCol).Background(amberCol)
	dimStyle   = lipgloss.NewStyle().Foreground(fadeCol)
	normStyle  = lipgloss.NewStyle().Foreground(inkCol)
	quarStyle  = lipgloss.NewStyle().Foreground(dangerCol)
	helpStyle  = lipgloss.NewStyle().Foreground(fadeCol).Italic(true)
)

// statusFilters cycled by tab.
var statusFilters = []string{"live", "active", "aging", "quarantined", "superseded", "tombstoned", "all"}

type model struct {
	s        *store.Store
	embedder embed.Embedder

	query      string
	inputMode  bool
	filterIdx  int
	items      []*store.Memory
	cursor     int
	offset     int
	width      int
	height     int
	detail     *store.Memory
	detailText string
	status     string
}

// Run starts the TUI.
func Run(s *store.Store, e embed.Embedder) error {
	m := model{s: s, embedder: e}
	m.reload()
	p := tea.NewProgram(&m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m *model) filterStatuses() []string {
	switch statusFilters[m.filterIdx] {
	case "live":
		return []string{store.StatusActive, store.StatusAging}
	case "all":
		return nil
	default:
		return []string{statusFilters[m.filterIdx]}
	}
}

func (m *model) reload() {
	m.cursor, m.offset = 0, 0
	if strings.TrimSpace(m.query) == "" {
		items, err := m.s.List(store.ListFilter{Statuses: m.filterStatuses(), Limit: 500})
		if err != nil {
			m.status = err.Error()
			return
		}
		m.items = items
		m.status = fmt.Sprintf("%d memories (%s)", len(items), statusFilters[m.filterIdx])
		return
	}
	rs, err := search.Recall(m.s, m.embedder, search.Request{Query: m.query, Limit: 100, History: true})
	if err != nil {
		m.status = err.Error()
		return
	}
	wanted := m.filterStatuses()
	var items []*store.Memory
	for _, r := range rs {
		if wanted == nil || contains(wanted, r.Memory.Status) {
			items = append(items, r.Memory)
		}
	}
	m.items = items
	m.status = fmt.Sprintf("%d hits for %q (%s)", len(items), m.query, statusFilters[m.filterIdx])
}

func contains(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}

func (m *model) Init() tea.Cmd { return nil }

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case tea.KeyMsg:
		if m.inputMode {
			switch msg.Type {
			case tea.KeyEnter:
				m.inputMode = false
				m.reload()
			case tea.KeyEsc:
				m.inputMode = false
				m.query = ""
				m.reload()
			case tea.KeyBackspace:
				if len(m.query) > 0 {
					m.query = m.query[:len(m.query)-1]
				}
			case tea.KeyRunes, tea.KeySpace:
				m.query += string(msg.Runes)
				if msg.Type == tea.KeySpace {
					m.query += " "
				}
			}
			return m, nil
		}
		if m.detail != nil {
			switch msg.String() {
			case "q", "esc", "enter":
				m.detail = nil
			case "a":
				if m.detail.Status == store.StatusQuarantined {
					_ = m.s.SetTrust(m.detail.ID, 1, store.OpApprove)
					_ = m.s.SetStatus(m.detail.ID, store.StatusActive, store.OpApprove, map[string]any{"via": "browse"})
					_ = m.s.ResolveFlags(m.detail.ID)
					m.status = "approved " + m.detail.ID[:8]
					m.detail = nil
					m.reload()
				}
			case "x":
				if m.detail.Status != store.StatusTombstoned {
					_ = m.s.SetStatus(m.detail.ID, store.StatusTombstoned, store.OpTombstone, map[string]any{"via": "browse"})
					m.status = "tombstoned " + m.detail.ID[:8] + " (restore reverses)"
					m.detail = nil
					m.reload()
				}
			}
			return m, nil
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "/":
			m.inputMode = true
		case "tab":
			m.filterIdx = (m.filterIdx + 1) % len(statusFilters)
			m.reload()
		case "shift+tab":
			m.filterIdx = (m.filterIdx + len(statusFilters) - 1) % len(statusFilters)
			m.reload()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "g":
			m.cursor = 0
		case "G":
			m.cursor = len(m.items) - 1
		case "enter":
			if m.cursor < len(m.items) {
				m.openDetail(m.items[m.cursor])
			}
		}
	}
	return m, nil
}

func (m *model) openDetail(mem *store.Memory) {
	full, err := m.s.Get(mem.ID)
	if err != nil {
		m.status = err.Error()
		return
	}
	m.detail = full
	var b strings.Builder
	now := time.Now().UTC()
	fmt.Fprintf(&b, "%s\n\n", titleStyle.Render(full.ID))
	fmt.Fprintf(&b, "%s\n\n", normStyle.Render(wrap(full.Content, max(40, m.width-8))))
	line := func(k, v string) { fmt.Fprintf(&b, "%s %s\n", dimStyle.Render(fmt.Sprintf("%-11s", k)), v) }
	line("type", full.Type)
	statusText := full.Status
	if full.SupersededBy != "" {
		statusText += " by " + full.SupersededBy[:8]
	}
	if full.Status == store.StatusQuarantined {
		statusText = quarStyle.Render(statusText)
	}
	line("status", statusText)
	line("trust", fmt.Sprintf("%s (%s)", full.Trust, full.Trust.Label()))
	line("importance", fmt.Sprintf("%d/5", full.Importance))
	line("confidence", fmt.Sprintf("%.2f stored · %.2f effective", full.Confidence, belief.EffectiveConfidence(full, now)))
	line("source", orDash(full.Source))
	line("created", full.CreatedAt.Format("2006-01-02 15:04"))
	line("confirmed", full.LastConfirmedAt.Format("2006-01-02 15:04"))
	if len(full.Entities) > 0 {
		var names []string
		for _, e := range full.Entities {
			names = append(names, e.Name)
		}
		line("entities", strings.Join(names, ", "))
	}
	if len(full.Tags) > 0 {
		line("tags", strings.Join(full.Tags, ", "))
	}
	if fs, _ := m.s.FlagsFor(full.ID); len(fs) > 0 {
		for _, f := range fs {
			line("flag", quarStyle.Render(f.Kind+": "+f.Detail))
		}
	}
	older, newer, _ := m.s.Chain(full.ID)
	if len(older)+len(newer) > 0 {
		b.WriteString("\n" + dimStyle.Render("chain:") + "\n")
		for i := len(older) - 1; i >= 0; i-- {
			fmt.Fprintf(&b, "  ← %s %s\n", older[i].ID[:8], dimStyle.Render(clip(older[i].Content, 70)))
		}
		fmt.Fprintf(&b, "  ● %s (this)\n", full.ID[:8])
		for _, n := range newer {
			fmt.Fprintf(&b, "  → %s %s\n", n.ID[:8], dimStyle.Render(clip(n.Content, 70)))
		}
	}
	actions := "[q] back"
	if full.Status == store.StatusQuarantined {
		actions += "   [a] approve → T1 active"
	}
	if full.Status != store.StatusTombstoned {
		actions += "   [x] tombstone"
	}
	b.WriteString("\n" + helpStyle.Render(actions))
	m.detailText = b.String()
}

func (m *model) View() string {
	if m.width == 0 {
		m.width, m.height = 100, 30
	}
	if m.detail != nil {
		return lipgloss.NewStyle().Padding(1, 2).Render(m.detailText)
	}
	var b strings.Builder
	header := titleStyle.Render("amber browse")
	filter := dimStyle.Render(" · filter: " + statusFilters[m.filterIdx])
	q := ""
	if m.inputMode {
		q = normStyle.Render("  /" + m.query + "▌")
	} else if m.query != "" {
		q = dimStyle.Render("  /" + m.query)
	}
	b.WriteString(header + filter + q + "\n\n")

	rows := m.height - 6
	if rows < 5 {
		rows = 5
	}
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+rows {
		m.offset = m.cursor - rows + 1
	}
	end := min(len(m.items), m.offset+rows)
	for i := m.offset; i < end; i++ {
		mem := m.items[i]
		badge := " "
		switch mem.Status {
		case store.StatusQuarantined:
			badge = quarStyle.Render("Q")
		case store.StatusAging:
			badge = dimStyle.Render("a")
		case store.StatusSuperseded:
			badge = dimStyle.Render("s")
		case store.StatusTombstoned:
			badge = dimStyle.Render("t")
		}
		line := fmt.Sprintf("%s %s %s %-10s %s", badge, mem.ID[:8], mem.Trust, mem.Type, clip(mem.Content, max(20, m.width-30)))
		if i == m.cursor {
			b.WriteString(selStyle.Render(line) + "\n")
		} else {
			b.WriteString(normStyle.Render(line) + "\n")
		}
	}
	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  (empty)") + "\n")
	}
	b.WriteString("\n" + dimStyle.Render(m.status) + "\n")
	b.WriteString(helpStyle.Render("[/] search  [tab] status filter  [enter] inspect  [j/k] move  [q] quit"))
	return lipgloss.NewStyle().Padding(0, 1).Render(b.String())
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > n {
		return s[:n-1] + "…"
	}
	return s
}

func wrap(s string, width int) string {
	words := strings.Fields(s)
	var b strings.Builder
	line := 0
	for _, w := range words {
		if line+len(w)+1 > width {
			b.WriteString("\n")
			line = 0
		} else if line > 0 {
			b.WriteString(" ")
			line++
		}
		b.WriteString(w)
		line += len(w)
	}
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
