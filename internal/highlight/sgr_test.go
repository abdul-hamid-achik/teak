package highlight

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"teak/internal/ui"
)

// The fast path exists only if it is indistinguishable from lipgloss. Any
// divergence would show up as wrong colours on screen, so every style the
// highlighter can emit is compared byte for byte against Style.Render.
func TestFastSGRMatchesLipglossForEveryStyle(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())

	samples := []string{
		"",
		"x",
		"func",
		"hello world",
		"áé漢字",
		"emoji 🙂 tail",
		"\ttabbed",
		"  leading and trailing  ",
	}

	checked := 0
	for tokenType, style := range h.styleMap {
		pair := h.sgrMap[tokenType]
		if !pair.fast {
			t.Errorf("token type %v has no fast path; expected all theme styles to qualify", tokenType)
			continue
		}
		tok := newStyledToken("", style, pair)
		for _, sample := range samples {
			if got, want := tok.Render(sample), style.Render(sample); got != want {
				t.Errorf("token type %v, sample %q:\n fast = %q\n slow = %q", tokenType, sample, got, want)
			}
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no styles were checked")
	}
	t.Logf("verified %d styles against lipgloss", checked)
}

func TestFallbackStyleUsesFastPath(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())

	// An unknown token type falls back to the editor style, which must also be
	// covered by the fast path rather than silently degrading every token.
	style, pair := h.resolveToken(0)
	if !pair.fast {
		t.Error("fallback editor style has no fast path")
	}
	tok := newStyledToken("", style, pair)
	if got, want := tok.Render("fallback"), style.Render("fallback"); got != want {
		t.Errorf("fallback render = %q, want %q", got, want)
	}
}

func TestStyledTokenWriteToMatchesRender(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())
	style, pair := h.resolveToken(0)
	tok := newStyledToken("", style, pair)
	for _, sample := range []string{"identifier", "áé漢字", "\ttabbed"} {
		var b strings.Builder
		tok.WriteTo(&b, sample)
		if got, want := b.String(), tok.Render(sample); got != want {
			t.Fatalf("WriteTo(%q) = %q, want %q", sample, got, want)
		}
	}
}

func TestStyledTokenWithBackgroundMatchesLipgloss(t *testing.T) {
	h := New("main.go", ui.DefaultTheme())
	style, pair := h.resolveToken(0)
	tok := newStyledToken("", style, pair)
	background := ui.Nord11
	withBackground := tok.WithBackground(background)
	wantStyle := style.Background(background)

	for _, sample := range []string{"identifier", "áé漢字", "\ttabbed"} {
		if got, want := withBackground.Render(sample), wantStyle.Render(sample); got != want {
			t.Fatalf("WithBackground(%q) = %q, want %q", sample, got, want)
		}
	}
}

func TestDeriveSGRRejectsContentDependentStyle(t *testing.T) {
	// A fixed-width style pads according to the content length, so no constant
	// prefix/suffix can reproduce it. Accepting one would truncate or misalign.
	style := lipgloss.NewStyle().Width(20).Foreground(ui.Nord8)

	if pair := deriveSGR(style); pair.fast {
		t.Error("deriveSGR accepted a width-constrained style; Render depends on content length")
	}
}

func TestStyledTokenFallsBackWhenNotFast(t *testing.T) {
	style := lipgloss.NewStyle().Width(20).Foreground(ui.Nord8)
	tok := newStyledToken("", style, deriveSGR(style))

	if got, want := tok.Render("abc"), style.Render("abc"); got != want {
		t.Errorf("render = %q, want lipgloss output %q", got, want)
	}
}

func BenchmarkTokenRenderFast(b *testing.B) {
	h := New("main.go", ui.DefaultTheme())
	style, pair := h.resolveToken(0)
	tok := newStyledToken("", style, pair)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = tok.Render("identifier")
	}
}

func BenchmarkTokenRenderLipgloss(b *testing.B) {
	h := New("main.go", ui.DefaultTheme())
	style, _ := h.resolveToken(0)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_ = style.Render("identifier")
	}
}
