package overlay

import (
	"unicode"
	"unicode/utf8"
)

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

	queryOffset := 0
	consecutive := 0
	prevMatchIdx := -1
	candidateRuneIdx := 0
	var prevCandidateRune rune
	havePrevCandidateRune := false

	for _, candidateRune := range candidate {
		if queryOffset >= len(query) {
			break
		}
		queryRune, querySize := utf8.DecodeRuneInString(query[queryOffset:])
		if unicode.ToLower(candidateRune) != unicode.ToLower(queryRune) {
			consecutive = 0
			prevCandidateRune = candidateRune
			havePrevCandidateRune = true
			candidateRuneIdx++
			continue
		}

		// Character match
		score += 1

		// Exact case bonus
		if candidateRune == queryRune {
			score += 1
		}

		// Consecutive bonus
		if prevMatchIdx == candidateRuneIdx-1 {
			consecutive++
			score += consecutive * 2
		} else {
			consecutive = 1
		}

		// Start-of-word bonus
		if candidateRuneIdx == 0 || (havePrevCandidateRune && isSeparator(prevCandidateRune)) {
			score += 5
		}

		// First char of query matching first char of candidate
		if queryOffset == 0 && candidateRuneIdx == 0 {
			score += 10
		}

		prevMatchIdx = candidateRuneIdx
		queryOffset += querySize
		prevCandidateRune = candidateRune
		havePrevCandidateRune = true
		candidateRuneIdx++
	}

	if queryOffset < len(query) {
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

	positions := make([]int, 0, len(query))
	queryOffset := 0

	for candidateOffset, candidateRune := range candidate {
		if queryOffset >= len(query) {
			break
		}
		queryRune, querySize := utf8.DecodeRuneInString(query[queryOffset:])
		if unicode.ToLower(candidateRune) == unicode.ToLower(queryRune) {
			positions = append(positions, candidateOffset)
			queryOffset += querySize
		}
	}

	if queryOffset < len(query) {
		return nil
	}
	return positions
}

func isSeparator(r rune) bool {
	switch r {
	case '/', '.', '_', '-', ' ', '\\':
		return true
	}
	return false
}
