package overlay

import "strings"

// FuzzyMatch scores how well query matches candidate using fuzzy matching.
// Returns the score (higher is better) and whether all query characters were
// found in order. A score of 0 with matched=false means no match.
//
// Scoring bonuses:
//   - Consecutive character matches
//   - Match at start of candidate or after a separator (/, ., _, -, space)
//   - Exact case match
//   - Shorter candidates rank higher (less noise)
func FuzzyMatch(query, candidate string) (score int, matched bool) {
	if query == "" {
		return 0, true
	}
	if candidate == "" {
		return 0, false
	}

	qLower := strings.ToLower(query)
	cLower := strings.ToLower(candidate)

	qi := 0
	consecutive := 0
	prevMatchIdx := -1

	for ci := 0; ci < len(cLower) && qi < len(qLower); ci++ {
		if cLower[ci] != qLower[qi] {
			consecutive = 0
			continue
		}

		// Character match
		score += 1

		// Exact case bonus
		if candidate[ci] == query[qi] {
			score += 1
		}

		// Consecutive bonus
		if prevMatchIdx == ci-1 {
			consecutive++
			score += consecutive * 2
		} else {
			consecutive = 1
		}

		// Start-of-word bonus
		if ci == 0 || isSeparator(candidate[ci-1]) {
			score += 5
		}

		// First char of query matching first char of candidate
		if qi == 0 && ci == 0 {
			score += 10
		}

		prevMatchIdx = ci
		qi++
	}

	if qi < len(qLower) {
		return 0, false
	}

	// Shorter candidates get a small bonus (less noise)
	if len(candidate) > 0 {
		score += 10 / len(candidate)
	}

	return score, true
}

// MatchPositions returns the 0-based byte indices in candidate where each
// query character matched, using the same greedy left-to-right strategy as
// FuzzyMatch. Returns nil if not all query characters match.
func MatchPositions(query, candidate string) []int {
	if query == "" {
		return []int{}
	}
	if candidate == "" {
		return nil
	}

	qLower := strings.ToLower(query)
	cLower := strings.ToLower(candidate)

	positions := make([]int, 0, len(query))
	qi := 0

	for ci := 0; ci < len(cLower) && qi < len(qLower); ci++ {
		if cLower[ci] == qLower[qi] {
			positions = append(positions, ci)
			qi++
		}
	}

	if qi < len(qLower) {
		return nil
	}
	return positions
}

func isSeparator(b byte) bool {
	switch b {
	case '/', '.', '_', '-', ' ', '\\':
		return true
	}
	return false
}
