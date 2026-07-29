package screen

import (
	"slices"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/njyeung/reels/tui/colors"
)

// assertStable checks that painting a screen's own Render output back into a
// fresh screen reproduces the same cells.
//
// This is the invariant to reach for whenever lipgloss output can't be compared
// byte-for-byte: lipgloss re-emits the full SGR run per character for some
// attribute combinations, whereas Render coalesces a repeated style into one
// run. The bytes differ, the meaning must not. Objects and zones aren't carried
// in the string, so only the visual fields are compared.
func assertStable(t *testing.T, s *Screen) {
	t.Helper()
	round := New(s.Width(), s.Height())
	round.SetContent(round.Bounds(), s.Render(), nil)

	for y := range s.Height() {
		for x := range s.Width() {
			a, b := s.CellAt(x, y), round.CellAt(x, y)
			if a.Rune != b.Rune || a.Width != b.Width || a.Style != b.Style || !slices.Equal(a.Comb, b.Comb) {
				t.Errorf("cell(%d,%d) changed across a render round trip:\n  got  %+v\n  want %+v", x, y, *b, *a)
				return
			}
		}
	}
}

// occupied returns the column just past the last non-blank cell in row y, i.e.
// how many columns the text actually claimed.
func occupied(s *Screen, y int) int {
	end := 0
	for x := range s.Width() {
		if c := s.CellAt(x, y); !c.visuallyBlank() || c.Width == 0 {
			end = x + 1
		}
	}
	return end
}

// Comment authors and captions are arbitrary user text, so the matrix has to
// survive every script Instagram allows. The invariant that matters is that
// text round-trips through the cells byte-for-byte and claims the number of
// columns the width table says it should — if either breaks, every zone and
// every reserved image to the right of it shifts.
func TestScriptsRoundTrip(t *testing.T) {
	tests := []struct {
		name    string
		text    string
		columns int
	}{
		{"chinese", "你好世界", 8},
		{"korean precomposed", "안녕하세요", 10},
		{"korean decomposed jamo", "\u1112\u1161\u11ab", 2}, // 한, as three jamo
		{"japanese", "こんにちは", 10},
		{"halfwidth katakana", "ﾊﾛｰ", 3},
		{"fullwidth punctuation", "！？", 4},
		{"arabic", "مرحبا", 5},
		{"arabic with harakat", "مَرْحَبًا", 5},
		{"hebrew", "שלום", 4},
		{"thai", "สวัสดี", 4},
		{"devanagari", "नमस्ते", 4},
		{"zwj family", "👨‍👩‍👧‍👦", 2},
		{"regional indicator flag", "🇯🇵", 2},
		{"emoji with skin tone", "👍🏽", 2},
		{"keycap", "1️⃣", 1},
		{"mixed scripts", "hi 你好 مرحبا 🇯🇵", 16},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := New(40, 1)
			s.SetContent(s.Bounds(), tc.text, nil)

			if got := s.Render(); got != tc.text {
				t.Errorf("Render() = %q, want %q", got, tc.text)
			}
			if got := occupied(s, 0); got != tc.columns {
				t.Errorf("occupied %d columns, want %d", got, tc.columns)
			}
		})
	}
}

// Decomposed Hangul is three runes that the segmenter folds into one
// double-width syllable.
func TestKoreanJamoIsOneGrapheme(t *testing.T) {
	s := New(10, 1)
	s.SetContent(s.Bounds(), "\u1112\u1161\u11ab", nil) // 한, as three jamo

	head := s.CellAt(0, 0)
	if head.Rune != 'ᄒ' {
		t.Errorf("head.Rune = %U, want U+1112", head.Rune)
	}
	if len(head.Comb) != 2 {
		t.Errorf("head.Comb = %U, want the two trailing jamo", head.Comb)
	}
	if head.Width != 2 {
		t.Errorf("head.Width = %d, want 2", head.Width)
	}
	if got := s.CellAt(1, 0).Width; got != 0 {
		t.Errorf("cell(1,0).Width = %d, want 0 (continuation)", got)
	}
}

// A ZWJ sequence is seven runes and one grapheme. Splitting it would render as
// four separate people.
func TestZWJSequenceIsOneGrapheme(t *testing.T) {
	s := New(10, 1)
	s.SetContent(s.Bounds(), "👨‍👩‍👧‍👦", nil)

	head := s.CellAt(0, 0)
	if head.Width != 2 {
		t.Errorf("head.Width = %d, want 2", head.Width)
	}
	if len(head.Comb) != 6 {
		t.Errorf("len(head.Comb) = %d, want 6 (the ZWJs and the other three people)", len(head.Comb))
	}
	if got := occupied(s, 0); got != 2 {
		t.Errorf("occupied %d columns, want 2", got)
	}
}

// The keycap is an ASCII digit followed by U+FE0F U+20E3. ASCII short-circuits
// before grapheme clustering in the decoder, so the combining part arrives as
// its own zero-width sequence and has to be folded back in.
func TestKeycapFoldsOntoASCIIBase(t *testing.T) {
	s := New(10, 1)
	s.SetContent(s.Bounds(), "1️⃣x", nil)

	head := s.CellAt(0, 0)
	if head.Rune != '1' {
		t.Errorf("head.Rune = %q, want '1'", head.Rune)
	}
	if len(head.Comb) != 2 {
		t.Errorf("head.Comb = %U, want [U+FE0F U+20E3]", head.Comb)
	}
	if got := s.CellAt(1, 0).Rune; got != 'x' {
		t.Errorf("cell(1,0).Rune = %q, want 'x' — the combining part must not consume a cell", got)
	}
}

// Arabic diacritics ride inside their base letter's cluster rather than
// arriving separately, because the base is not ASCII.
func TestArabicHarakatStayInCluster(t *testing.T) {
	s := New(10, 1)
	s.SetContent(s.Bounds(), "مَرْحَبًا", nil)

	head := s.CellAt(0, 0)
	if head.Rune != 'م' {
		t.Errorf("head.Rune = %U, want U+0645 (meem)", head.Rune)
	}
	if len(head.Comb) != 1 {
		t.Errorf("head.Comb = %U, want one fatha", head.Comb)
	}
	if head.Width != 1 {
		t.Errorf("head.Width = %d, want 1", head.Width)
	}
}

// Terminals store RTL text in logical order and reorder at display time, so the
// matrix must too: cell 0 holds the first rune of the string, not the
// rightmost glyph. Anything else would make hit testing agree with neither.
func TestArabicStoredInLogicalOrder(t *testing.T) {
	s := New(10, 1)
	text := "مرحبا"
	s.SetContent(s.Bounds(), text, nil)

	for i, want := range []rune(text) {
		if got := s.CellAt(i, 0).Rune; got != want {
			t.Errorf("cell(%d,0).Rune = %U, want %U", i, got, want)
		}
	}
}

// A double-width CJK glyph must be dropped whole at the right edge rather than
// half-written, which would leave a continuation cell with no head.
func TestCJKTruncatesWholeGlyphs(t *testing.T) {
	s := New(5, 1)
	s.SetContent(s.Bounds(), "你好世", nil)

	if got := s.Render(); got != "你好" {
		t.Errorf("Render() = %q, want %q — the third glyph does not fit in the odd column", got, "你好")
	}
	if got := s.CellAt(4, 0).Rune; got != ' ' {
		t.Errorf("cell(4,0).Rune = %q, want blank", got)
	}
}

// Clicking either column of a double-width glyph has to resolve to the same
// target.
func TestZoneCoversBothCellsOfWideGlyph(t *testing.T) {
	s := New(10, 1)
	z := &Zone{Owner: OwnerComments, Target: 3}
	s.SetContent(s.Bounds(), "你好", z)

	for x := range 4 {
		if got := s.Hit(x, 0); got != z {
			t.Errorf("Hit(%d,0) = %v, want the comment zone", x, got)
		}
	}
	if got := s.Hit(4, 0); got != nil {
		t.Errorf("Hit(4,0) = %v, want nil", got)
	}
}

func TestStyledCJKRoundTrip(t *testing.T) {
	text := "你好世界"
	styled := lipgloss.NewStyle().Foreground(colors.Pink500Color).Render(text)

	s := New(20, 1)
	s.SetContent(s.Bounds(), styled, nil)

	// The style lands on the head cell and its continuation alike, so a
	// re-render can't lose it if the head is ever skipped.
	if s.CellAt(0, 0).Style == "" {
		t.Fatal("styled CJK head carries no style")
	}
	if s.CellAt(0, 0).Style != s.CellAt(1, 0).Style {
		t.Error("continuation cell should carry the head's style")
	}
	if got := s.Render(); got != styled {
		t.Errorf("Render() = %q, want %q", got, styled)
	}
}

// Real panel lines interleave several styled runs. Each has to land on exactly
// its own cells, with the reset between them clearing rather than accumulating.
func TestAdjacentStyledRuns(t *testing.T) {
	pink := lipgloss.NewStyle().Foreground(colors.Pink500Color)
	blue := lipgloss.NewStyle().Foreground(colors.Blue500Color)
	line := pink.Render("@ab") + " " + blue.Render("✓")

	s := New(20, 1)
	s.SetContent(s.Bounds(), line, nil)

	pinkStyle := s.CellAt(0, 0).Style
	blueStyle := s.CellAt(4, 0).Style

	switch {
	case pinkStyle == "":
		t.Error("first run lost its style")
	case blueStyle == "":
		t.Error("last run lost its style")
	case pinkStyle == blueStyle:
		t.Error("the two runs should not share a style")
	}
	for x := range 3 {
		if got := s.CellAt(x, 0).Style; got != pinkStyle {
			t.Errorf("cell(%d,0).Style = %q, want the first run's style", x, got)
		}
	}
	if got := s.CellAt(3, 0).Style; got != "" {
		t.Errorf("separating space Style = %q, want cleared by the reset", got)
	}
	if got := s.Render(); got != line {
		t.Errorf("Render() = %q, want %q", got, line)
	}
}

// Bold, italic, underline, foreground and background stack into one SGR run.
// Storing sequences verbatim means this works without the package knowing what
// any of the attributes mean.
func TestCombinedAttributesRoundTrip(t *testing.T) {
	styled := lipgloss.NewStyle().
		Bold(true).
		Italic(true).
		Underline(true).
		Foreground(colors.Yellow500Color).
		Background(colors.Purple900Color).
		Render("mix")

	s := New(20, 1)
	s.SetContent(s.Bounds(), styled, nil)

	if s.CellAt(0, 0).Style == "" {
		t.Fatal("combined attributes produced no style")
	}
	// lipgloss re-emits the whole SGR run per character here, which Render
	// collapses into one, so compare meaning rather than bytes.
	for x := range 3 {
		if got := s.CellAt(x, 0).Style; got != s.CellAt(0, 0).Style {
			t.Errorf("cell(%d,0).Style = %q, want every cell of the run to match", x, got)
		}
	}
	if got := stripSGR(s.Render()); got != "mix" {
		t.Errorf("Render() text = %q, want %q", got, "mix")
	}
	assertStable(t, s)
}

// stripSGR removes SGR sequences, leaving the printable text.
func stripSGR(s string) string {
	var b strings.Builder
	for len(s) > 0 {
		if rest, ok := strings.CutPrefix(s, "\x1b["); ok {
			if i := strings.IndexByte(rest, 'm'); i >= 0 {
				s = rest[i+1:]
				continue
			}
		}
		r, n := utf8.DecodeRuneInString(s)
		b.WriteRune(r)
		s = s[n:]
	}
	return b.String()
}

// A styled background must not be trimmed as if it were blank: the spaces are
// visible.
func TestStyledSpacesSurviveTrimming(t *testing.T) {
	styled := lipgloss.NewStyle().Background(colors.Purple500Color).Render("   ")

	s := New(10, 1)
	s.SetContent(s.Bounds(), styled, nil)

	if got := s.Render(); got != styled {
		t.Errorf("Render() = %q, want %q — styled spaces are not blanks", got, styled)
	}
}
