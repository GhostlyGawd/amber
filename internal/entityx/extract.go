// Package entityx extracts entity mentions from memory content with
// conservative heuristics. The LLM digest path supplies richer entities;
// this covers `amber remember` without an LLM in the loop.
package entityx

import (
	"regexp"
	"strings"
)

// Mention is a detected entity reference.
type Mention struct {
	Name string
	Type string // person|project|org|other
}

var (
	emailRe  = regexp.MustCompile(`\b([A-Za-z0-9._%+\-]+)@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`)
	handleRe = regexp.MustCompile(`(^|[\s(])@([A-Za-z0-9_\-]{2,})`)
	repoRe   = regexp.MustCompile(`\b([a-z0-9][a-z0-9_\-]+/[a-z0-9][a-z0-9._\-]+)\b`)
	orgSfxRe = regexp.MustCompile(`\b([A-Z][A-Za-z0-9&.\- ]{1,40}?\s(?:Inc|Corp|Corporation|Labs|LLC|Ltd|GmbH|AG|Co)\.?)(?:\s|$|[,.;:])`)
	// Capitalized runs of 1-3 words, not sentence-initial (handled by caller
	// passing position), e.g. "Rhen McLeod", "Postgres".
	properRe = regexp.MustCompile(`\b([A-Z][a-zA-Z0-9'\-]+(?:\s[A-Z][a-zA-Z0-9'\-]+){0,2})\b`)
)

// Common sentence-leading words and generic tech words we refuse to turn
// into entities on their own.
var stopwords = map[string]bool{
	"the": true, "a": true, "an": true, "i": true, "we": true, "he": true, "she": true,
	"they": true, "it": true, "user": true, "always": true, "never": true, "when": true,
	"if": true, "after": true, "before": true, "use": true, "uses": true, "using": true,
	"prefers": true, "team": true, "this": true, "that": true, "these": true, "those": true,
	"monday": true, "tuesday": true, "wednesday": true, "thursday": true, "friday": true,
	"saturday": true, "sunday": true, "january": true, "february": true, "march": true,
	"april": true, "may": true, "june": true, "july": true, "august": true, "september": true,
	"october": true, "november": true, "december": true, "yes": true, "no": true, "ok": true,
	"todo": true, "note": true, "preference": true, "decision": true, "fact": true,
}

// Extract returns entity mentions found in content. knownNames lets the
// caller pass existing entity names so recurring single-word mentions
// (even sentence-initial) keep linking.
func Extract(content string, knownNames []string) []Mention {
	seen := map[string]bool{}
	var out []Mention
	add := func(name, typ string) {
		name = strings.TrimSpace(strings.Trim(name, ".,;:!?"))
		if name == "" || stopwords[strings.ToLower(name)] {
			return
		}
		key := strings.ToLower(name)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Mention{Name: name, Type: typ})
	}

	for _, m := range emailRe.FindAllStringSubmatch(content, -1) {
		// The mailbox name is the entity; the address is an alias handled
		// by the caller.
		add(m[0], "person")
	}
	for _, m := range handleRe.FindAllStringSubmatch(content, -1) {
		add("@"+m[2], "person")
	}
	for _, m := range repoRe.FindAllStringSubmatch(content, -1) {
		if strings.Contains(m[1], ".") && !strings.Contains(m[1], "/") {
			continue
		}
		add(m[1], "project")
	}
	for _, m := range orgSfxRe.FindAllStringSubmatch(content, -1) {
		add(m[1], "org")
	}

	known := map[string]bool{}
	for _, k := range knownNames {
		known[strings.ToLower(k)] = true
	}

	for _, loc := range properRe.FindAllStringIndex(content, -1) {
		name := content[loc[0]:loc[1]]
		lower := strings.ToLower(name)
		if stopwords[lower] || seen[lower] {
			continue
		}
		sentenceInitial := isSentenceInitial(content, loc[0])
		words := strings.Fields(name)
		switch {
		case known[lower]:
			add(name, "other")
		case len(words) >= 2:
			// Multi-word capitalized runs are strong signals even at
			// sentence start ("Rhen McLeod prefers...").
			if allCapitalized(words) && !stopwords[strings.ToLower(words[0])] {
				add(name, guessType(name))
			}
		case !sentenceInitial:
			add(name, guessType(name))
		}
	}
	return out
}

func isSentenceInitial(content string, pos int) bool {
	for i := pos - 1; i >= 0; i-- {
		c := content[i]
		switch {
		case c == ' ' || c == '\t':
			continue
		case c == '.' || c == '!' || c == '?' || c == '\n' || c == ':' || c == '-':
			return true
		default:
			return false
		}
	}
	return true
}

func allCapitalized(words []string) bool {
	for _, w := range words {
		if w == "" || w[0] < 'A' || w[0] > 'Z' {
			return false
		}
	}
	return true
}

var personTitles = regexp.MustCompile(`^(Dr|Mr|Mrs|Ms|Prof)\.?\s`)

func guessType(name string) string {
	if personTitles.MatchString(name) {
		return "person"
	}
	words := strings.Fields(name)
	if len(words) == 2 && !strings.ContainsAny(name, "0123456789") {
		// Two capitalized words with no digits reads like a person name.
		return "person"
	}
	return "other"
}

// EmailAlias returns (displayName, email) when content ties a name to an
// address ("Alice Chen <alice@x.com>" or "alice@x.com"); used by the
// caller to register aliases.
func EmailAlias(content string) (string, string) {
	m := regexp.MustCompile(`([A-Z][a-zA-Z'\-]+(?:\s[A-Z][a-zA-Z'\-]+)+)\s*[<(]([A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,})[>)]`).FindStringSubmatch(content)
	if m != nil {
		return m[1], m[2]
	}
	return "", ""
}
