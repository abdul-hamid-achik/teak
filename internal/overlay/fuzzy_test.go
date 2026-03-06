package overlay

import (
	"testing"
)

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		candidate string
		wantMatch bool
		wantMin   int // minimum expected score (0 = just check matched)
	}{
		{
			name:      "empty query matches everything",
			query:     "",
			candidate: "anything",
			wantMatch: true,
			wantMin:   0,
		},
		{
			name:      "empty candidate no match",
			query:     "a",
			candidate: "",
			wantMatch: false,
		},
		{
			name:      "exact match",
			query:     "main.go",
			candidate: "main.go",
			wantMatch: true,
			wantMin:   1,
		},
		{
			name:      "prefix match",
			query:     "mai",
			candidate: "main.go",
			wantMatch: true,
			wantMin:   1,
		},
		{
			name:      "fuzzy match scattered chars",
			query:     "mgo",
			candidate: "main.go",
			wantMatch: true,
			wantMin:   1,
		},
		{
			name:      "case insensitive",
			query:     "MAIN",
			candidate: "main.go",
			wantMatch: true,
			wantMin:   1,
		},
		{
			name:      "no match",
			query:     "xyz",
			candidate: "main.go",
			wantMatch: false,
		},
		{
			name:      "query longer than candidate",
			query:     "maingofile",
			candidate: "main.go",
			wantMatch: false,
		},
		{
			name:      "separator bonus",
			query:     "fg",
			candidate: "foo/go",
			wantMatch: true,
			wantMin:   1,
		},
		{
			name:      "single char",
			query:     "m",
			candidate: "main.go",
			wantMatch: true,
			wantMin:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, matched := FuzzyMatch(tt.query, tt.candidate)
			if matched != tt.wantMatch {
				t.Errorf("FuzzyMatch(%q, %q) matched=%v, want %v", tt.query, tt.candidate, matched, tt.wantMatch)
			}
			if matched && score < tt.wantMin {
				t.Errorf("FuzzyMatch(%q, %q) score=%d, want >= %d", tt.query, tt.candidate, score, tt.wantMin)
			}
		})
	}
}

func TestFuzzyMatchScoreOrdering(t *testing.T) {
	// Exact prefix should score higher than scattered match
	scorePrefix, _ := FuzzyMatch("main", "main.go")
	scoreScattered, _ := FuzzyMatch("main", "my_awesome_internal_notes.go")
	if scorePrefix <= scoreScattered {
		t.Errorf("prefix match score (%d) should be > scattered match score (%d)", scorePrefix, scoreScattered)
	}

	// Exact case should score higher than different case
	scoreExact, _ := FuzzyMatch("Main", "Main.go")
	scoreLower, _ := FuzzyMatch("Main", "main.go")
	if scoreExact <= scoreLower {
		t.Errorf("exact case score (%d) should be > case-insensitive score (%d)", scoreExact, scoreLower)
	}
}

func TestFuzzyMatchSpecialCharacters(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		candidate string
		wantMatch bool
	}{
		{"dot in query", "a.g", "app.go", true},
		{"slash in query", "i/a", "internal/app", true},
		{"underscore in query", "m_f", "my_func", true},
		{"hyphen in query", "f-b", "foo-bar", true},
		{"backslash separator", "a\\b", "a\\b", true},
		{"numbers", "123", "file123.go", true},
		{"mixed symbols", "f.g", "foo.go", true},
		{"no match with special", "x.y", "abc", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, matched := FuzzyMatch(tt.query, tt.candidate)
			if matched != tt.wantMatch {
				t.Errorf("FuzzyMatch(%q, %q) matched=%v, want %v", tt.query, tt.candidate, matched, tt.wantMatch)
			}
		})
	}
}

func TestFuzzyMatchUnicode(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		candidate string
		wantMatch bool
	}{
		{"ascii skips multibyte", "hllo", "héllo", true}, // byte-level matching: h matches, then l,l,o found
		{"same ascii chars", "abc", "abc_日本語", true},
		{"pure ascii subset", "file", "file_名前.go", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, matched := FuzzyMatch(tt.query, tt.candidate)
			if matched != tt.wantMatch {
				t.Errorf("FuzzyMatch(%q, %q) matched=%v, want %v", tt.query, tt.candidate, matched, tt.wantMatch)
			}
		})
	}
}

func TestFuzzyMatchSeparatorBonus(t *testing.T) {
	// Characters after separators should score higher
	scoreSep, _ := FuzzyMatch("ag", "app.go")
	scoreNoSep, _ := FuzzyMatch("ag", "abcgo")
	if scoreSep <= scoreNoSep {
		t.Errorf("separator match (%d) should score higher than non-separator (%d)", scoreSep, scoreNoSep)
	}
}

func TestFuzzyMatchBothEmpty(t *testing.T) {
	score, matched := FuzzyMatch("", "")
	if !matched {
		t.Error("empty query on empty candidate should match")
	}
	if score != 0 {
		t.Errorf("score = %d, want 0", score)
	}
}

func TestMatchPositions(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		candidate string
		want      []int
	}{
		{
			name:      "empty query",
			query:     "",
			candidate: "anything",
			want:      []int{},
		},
		{
			name:      "empty candidate",
			query:     "a",
			candidate: "",
			want:      nil,
		},
		{
			name:      "exact prefix",
			query:     "mai",
			candidate: "main.go",
			want:      []int{0, 1, 2},
		},
		{
			name:      "scattered",
			query:     "mgo",
			candidate: "main.go",
			want:      []int{0, 5, 6},
		},
		{
			name:      "case insensitive positions",
			query:     "MG",
			candidate: "main.go",
			want:      []int{0, 5},
		},
		{
			name:      "no match returns nil",
			query:     "xyz",
			candidate: "main.go",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MatchPositions(tt.query, tt.candidate)
			if tt.want == nil {
				if got != nil {
					t.Errorf("MatchPositions(%q, %q) = %v, want nil", tt.query, tt.candidate, got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Errorf("MatchPositions(%q, %q) = %v, want %v", tt.query, tt.candidate, got, tt.want)
				return
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("MatchPositions(%q, %q)[%d] = %d, want %d", tt.query, tt.candidate, i, got[i], tt.want[i])
				}
			}
		})
	}
}
