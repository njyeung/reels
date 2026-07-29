package screen

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
	"github.com/njyeung/reels/tui/colors"
)

var (
	update    = flag.Bool("update", false, "rewrite golden files")
	printDump = flag.Bool("print", false, "print the sample frame to stdout")
)

// TestMain pins lipgloss to a full color profile. Without this, lipgloss sees
// no TTY under `go test` and degrades to Ascii, emitting no escape sequences at
// all — the style tests would pass vacuously and the golden file would differ
// between a terminal and CI.
func TestMain(m *testing.M) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	os.Exit(m.Run())
}

// full is the whole screen, for tests that don't care about clipping.
func full(s *Screen) Rect { return s.Bounds() }

func TestSetContentASCII(t *testing.T) {
	s := New(10, 2)
	s.SetContent(full(s), "hi", nil)

	if got := s.CellAt(0, 0).Rune; got != 'h' {
		t.Errorf("cell(0,0).Rune = %q, want 'h'", got)
	}
	if got := s.CellAt(1, 0).Rune; got != 'i' {
		t.Errorf("cell(1,0).Rune = %q, want 'i'", got)
	}
	if got := s.CellAt(2, 0).Rune; got != ' ' {
		t.Errorf("cell(2,0).Rune = %q, want a blank", got)
	}
	if got := s.CellAt(0, 0).Width; got != 1 {
		t.Errorf("cell(0,0).Width = %d, want 1", got)
	}
}

func TestSetContentNewlineAdvancesWithinRect(t *testing.T) {
	s := New(10, 3)
	r := Rect{X: 2, Y: 1, W: 6, H: 2}
	s.SetContent(r, "ab\ncd", nil)

	// Both lines start at the rect's left edge, not column 0.
	for _, tc := range []struct {
		x, y int
		want rune
	}{
		{2, 1, 'a'}, {3, 1, 'b'},
		{2, 2, 'c'}, {3, 2, 'd'},
		{0, 1, ' '}, {1, 1, ' '},
	} {
		if got := s.CellAt(tc.x, tc.y).Rune; got != tc.want {
			t.Errorf("cell(%d,%d).Rune = %q, want %q", tc.x, tc.y, got, tc.want)
		}
	}
}

func TestSetContentWideGrapheme(t *testing.T) {
	s := New(6, 1)
	s.SetContent(full(s), "🤍x", nil)

	head := s.CellAt(0, 0)
	if head.Rune != '🤍' || head.Width != 2 {
		t.Errorf("head = {%q, w=%d}, want {'🤍', w=2}", head.Rune, head.Width)
	}

	cont := s.CellAt(1, 0)
	if cont.Rune != 0 || cont.Width != 0 {
		t.Errorf("continuation = {%q, w=%d}, want {0, w=0}", cont.Rune, cont.Width)
	}

	// The next glyph lands past the wide one, not on top of its tail.
	if got := s.CellAt(2, 0).Rune; got != 'x' {
		t.Errorf("cell(2,0).Rune = %q, want 'x'", got)
	}
}

// The status line's heart is U+2764 U+FE0F. The variation selector must ride
// along in Comb, and the pair must claim two cells, or every icon after it
// shifts.
func TestSetContentVariationSelector(t *testing.T) {
	s := New(6, 1)
	s.SetContent(full(s), "❤️", nil)

	head := s.CellAt(0, 0)
	if head.Rune != '❤' {
		t.Errorf("head.Rune = %U, want U+2764", head.Rune)
	}
	if len(head.Comb) != 1 || head.Comb[0] != '️' {
		t.Errorf("head.Comb = %U, want [U+FE0F]", head.Comb)
	}
	if head.Width != 2 {
		t.Errorf("head.Width = %d, want 2", head.Width)
	}
	if got := s.CellAt(1, 0); got.Width != 0 {
		t.Errorf("cell(1,0).Width = %d, want 0 (continuation)", got.Width)
	}
}

// An ASCII base plus a combining mark is returned by the decoder as two
// separate sequences, because ASCII printables short-circuit before grapheme
// clustering. The mark has to be folded back into the base cell.
func TestSetContentCombiningMarkAfterASCII(t *testing.T) {
	s := New(6, 1)
	s.SetContent(full(s), "éf", nil)

	head := s.CellAt(0, 0)
	if head.Rune != 'e' {
		t.Errorf("head.Rune = %q, want 'e'", head.Rune)
	}
	if len(head.Comb) != 1 || head.Comb[0] != '́' {
		t.Errorf("head.Comb = %U, want [U+0301]", head.Comb)
	}
	if head.Width != 1 {
		t.Errorf("head.Width = %d, want 1", head.Width)
	}
	if got := s.CellAt(1, 0).Rune; got != 'f' {
		t.Errorf("cell(1,0).Rune = %q, want 'f' (mark must not consume a cell)", got)
	}
	if got := s.Render(); got != "éf" {
		t.Errorf("Render() = %q, want %q", got, "éf")
	}
}

func TestSetContentTruncatesRatherThanWraps(t *testing.T) {
	s := New(10, 2)
	r := Rect{X: 0, Y: 0, W: 3, H: 2}
	s.SetContent(r, "abcdef", nil)

	if got := s.CellAt(3, 0).Rune; got != ' ' {
		t.Errorf("cell(3,0).Rune = %q, want blank (must not exceed rect)", got)
	}
	if got := s.CellAt(0, 1).Rune; got != ' ' {
		t.Errorf("cell(0,1).Rune = %q, want blank (must not wrap)", got)
	}
	if got := s.Render(); got != "abc\n" {
		t.Errorf("Render() = %q, want %q", got, "abc\n")
	}
}

// A double-width glyph straddling the right edge is dropped whole rather than
// half-written, and does not shift what follows it.
func TestSetContentWideGlyphStraddlingEdge(t *testing.T) {
	s := New(3, 1)
	// The heart needs columns 2 and 3, but column 3 does not exist.
	s.SetContent(Rect{W: 3, H: 1}, "ab🤍", nil)

	if got := s.CellAt(2, 0).Rune; got != ' ' {
		t.Errorf("cell(2,0).Rune = %q, want blank — a wide glyph must not be half-written", got)
	}
	if got := s.Render(); got != "ab" {
		t.Errorf("Render() = %q, want %q", got, "ab")
	}
}

func TestSetContentClipsBelowRect(t *testing.T) {
	s := New(10, 5)
	r := Rect{X: 0, Y: 0, W: 10, H: 2}
	s.SetContent(r, "a\nb\nc", nil)

	if got := s.CellAt(0, 2).Rune; got != ' ' {
		t.Errorf("cell(0,2).Rune = %q, want blank (row 2 is outside the rect)", got)
	}
}

func TestSetContentClipsToScreen(t *testing.T) {
	s := New(4, 2)
	// Rect extends past the screen on both axes.
	s.SetContent(Rect{X: 2, Y: 1, W: 10, H: 10}, "abcdef", nil)

	if got := s.CellAt(2, 1).Rune; got != 'a' {
		t.Errorf("cell(2,1).Rune = %q, want 'a'", got)
	}
	if got := s.CellAt(3, 1).Rune; got != 'b' {
		t.Errorf("cell(3,1).Rune = %q, want 'b'", got)
	}
	// 'c' onward fall off the right edge; nothing panics and nothing wraps.
	if got := s.Render(); got != "\n  ab" {
		t.Errorf("Render() = %q, want %q", got, "\n  ab")
	}
}

func TestStyleRoundTrip(t *testing.T) {
	styled := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Render("hi")

	s := New(10, 1)
	s.SetContent(full(s), styled, nil)

	if s.CellAt(0, 0).Style == "" {
		t.Fatal("styled cell carries no style")
	}
	if s.CellAt(0, 0).Style != s.CellAt(1, 0).Style {
		t.Error("both cells of one styled run should carry the same style")
	}

	got := s.Render()
	if !strings.Contains(got, "hi") {
		t.Errorf("Render() = %q, want it to contain the text", got)
	}
	if !strings.HasSuffix(got, "\x1b[0m") {
		t.Errorf("Render() = %q, want a trailing reset", got)
	}
}

func TestStyleResetClearsStyle(t *testing.T) {
	s := New(10, 1)
	// Explicit SGR: red, then reset, then a plain character.
	s.SetContent(full(s), "\x1b[31ma\x1b[0mb", nil)

	if got := s.CellAt(0, 0).Style; got != "\x1b[31m" {
		t.Errorf("cell(0,0).Style = %q, want the red SGR", got)
	}
	if got := s.CellAt(1, 0).Style; got != "" {
		t.Errorf("cell(1,0).Style = %q, want cleared by the reset", got)
	}
}

func TestStyleAccumulates(t *testing.T) {
	s := New(10, 1)
	// SGR is cumulative: bold then red leaves a cell that is both.
	s.SetContent(full(s), "\x1b[1m\x1b[31ma", nil)

	if got := s.CellAt(0, 0).Style; got != "\x1b[1m\x1b[31m" {
		t.Errorf("cell(0,0).Style = %q, want both sequences folded together", got)
	}
}

// Blank rows must render as nothing. The player draws the video over those
// rows outside bubbletea's frame, so padding them with spaces would erase it.
func TestRenderTrimsTrailingBlanks(t *testing.T) {
	s := New(20, 3)
	s.SetContent(Rect{X: 0, Y: 1, W: 20, H: 1}, "x", nil)

	want := "\nx\n"
	if got := s.Render(); got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestRenderStyleDoesNotBleedAcrossRows(t *testing.T) {
	s := New(4, 2)
	s.SetContent(Rect{W: 4, H: 1}, "\x1b[31ma", nil)
	s.SetContent(Rect{Y: 1, W: 4, H: 1}, "b", nil)

	got := s.Render()
	want := "\x1b[31ma\x1b[0m\nb"
	if got != want {
		t.Errorf("Render() = %q, want %q", got, want)
	}
}

func TestReserveAndExtents(t *testing.T) {
	s := New(20, 10)
	obj := &Object{Kind: ObjVideo}
	r := Rect{X: 2, Y: 1, W: 5, H: 3}
	s.Reserve(r, obj)

	if obj.Want != r {
		t.Errorf("obj.Want = %s, want %s", obj.Want, r)
	}

	ext := s.Extents()
	if len(ext) != 1 {
		t.Fatalf("len(Extents()) = %d, want 1", len(ext))
	}
	if ext[0].Obj != obj {
		t.Error("extent points at the wrong object")
	}
	if ext[0].Visible != r {
		t.Errorf("Visible = %s, want %s", ext[0].Visible, r)
	}
	if ext[0].Clipped() {
		t.Error("Clipped() = true, want false for a fully visible object")
	}
}

func TestReserveClippedReportsSurvivingExtent(t *testing.T) {
	s := New(10, 5)
	obj := &Object{Kind: ObjGif}
	// Hangs off the bottom-right corner.
	s.Reserve(Rect{X: 8, Y: 3, W: 6, H: 6}, obj)

	ext := s.Extents()
	if len(ext) != 1 {
		t.Fatalf("len(Extents()) = %d, want 1", len(ext))
	}
	want := Rect{X: 8, Y: 3, W: 2, H: 2}
	if ext[0].Visible != want {
		t.Errorf("Visible = %s, want %s", ext[0].Visible, want)
	}
	if !ext[0].Clipped() {
		t.Error("Clipped() = false, want true")
	}
}

func TestReserveEntirelyOffScreenDisappears(t *testing.T) {
	s := New(10, 5)
	s.Reserve(Rect{X: 20, Y: 20, W: 3, H: 3}, &Object{Kind: ObjImage})

	if got := s.Extents(); len(got) != 0 {
		t.Errorf("len(Extents()) = %d, want 0 for an off-screen object", len(got))
	}
}

// Writing text over a reservation must not evict it: the reel's caption can sit
// on top of the video's rows without unreserving them.
func TestSetContentPreservesReservation(t *testing.T) {
	s := New(10, 2)
	obj := &Object{Kind: ObjVideo}
	s.Reserve(Rect{W: 10, H: 2}, obj)
	s.SetContent(Rect{W: 10, H: 1}, "hi", nil)

	if got := s.CellAt(0, 0).Obj; got != obj {
		t.Error("writing text dropped the object reservation")
	}
	if got := s.CellAt(0, 0).Rune; got != 'h' {
		t.Errorf("cell(0,0).Rune = %q, want 'h'", got)
	}
}

func TestExtentsOrderIsFirstSeen(t *testing.T) {
	s := New(10, 4)
	second := &Object{Kind: ObjGif}
	first := &Object{Kind: ObjImage}
	// Reserved out of order, but `first` occupies an earlier row.
	s.Reserve(Rect{X: 0, Y: 2, W: 2, H: 1}, second)
	s.Reserve(Rect{X: 0, Y: 0, W: 2, H: 1}, first)

	ext := s.Extents()
	if len(ext) != 2 {
		t.Fatalf("len(Extents()) = %d, want 2", len(ext))
	}
	if ext[0].Obj != first || ext[1].Obj != second {
		t.Error("Extents() should be ordered by first cell encountered, row-major")
	}
}

func TestZoneStamping(t *testing.T) {
	s := New(20, 1)
	like := &Zone{Owner: OwnerRoot, Target: 1}
	s.SetContent(Rect{W: 20, H: 1}, "❤️ 12", like)

	// Every cell of the run belongs to the zone, including the tail of the
	// double-width heart — clicking either half must hit the same target.
	for x := range 5 {
		if got := s.Hit(x, 0); got != like {
			t.Errorf("Hit(%d,0) = %v, want the like zone", x, got)
		}
	}
	if got := s.Hit(5, 0); got != nil {
		t.Errorf("Hit(5,0) = %v, want nil past the end of the text", got)
	}
}

func TestHitOutOfBounds(t *testing.T) {
	s := New(4, 2)
	for _, p := range [][2]int{{-1, 0}, {0, -1}, {4, 0}, {0, 2}} {
		if got := s.Hit(p[0], p[1]); got != nil {
			t.Errorf("Hit(%d,%d) = %v, want nil", p[0], p[1], got)
		}
	}
}

func TestClearRemovesObjectsAndZones(t *testing.T) {
	s := New(10, 2)
	s.Reserve(Rect{W: 4, H: 1}, &Object{Kind: ObjVideo})
	s.SetContent(Rect{W: 10, H: 1}, "hi", &Zone{Owner: OwnerRoot})
	s.Clear()

	if got := s.Extents(); len(got) != 0 {
		t.Errorf("len(Extents()) = %d after Clear, want 0", len(got))
	}
	if got := s.Hit(0, 0); got != nil {
		t.Errorf("Hit(0,0) = %v after Clear, want nil", got)
	}
	if got := s.Render(); got != "\n" {
		t.Errorf("Render() = %q after Clear, want a blank frame", got)
	}
}

func TestResizeClears(t *testing.T) {
	s := New(10, 2)
	s.SetContent(full(s), "hi", nil)
	s.Resize(4, 5)

	if s.Width() != 4 || s.Height() != 5 {
		t.Errorf("size = %dx%d, want 4x5", s.Width(), s.Height())
	}
	if got := s.CellAt(0, 0).Rune; got != ' ' {
		t.Errorf("cell(0,0).Rune = %q after Resize, want blank", got)
	}
}

func TestRectIntersect(t *testing.T) {
	tests := []struct {
		name string
		a, b Rect
		want Rect
	}{
		{"overlap", Rect{0, 0, 4, 4}, Rect{2, 2, 4, 4}, Rect{2, 2, 2, 2}},
		{"disjoint", Rect{0, 0, 2, 2}, Rect{5, 5, 2, 2}, Rect{}},
		{"touching edges", Rect{0, 0, 2, 2}, Rect{2, 0, 2, 2}, Rect{}},
		{"contained", Rect{0, 0, 10, 10}, Rect{3, 3, 2, 2}, Rect{3, 3, 2, 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Intersect(tc.b); got != tc.want {
				t.Errorf("Intersect() = %s, want %s", got, tc.want)
			}
		})
	}
}

// The screen is 0-based; the player's slots are 1-based. This pins the
// conversion the reconcile step in package tui has to perform, so that if the
// convention ever changes, it changes here first.
func TestExtentToPlayerCoordinates(t *testing.T) {
	s := New(80, 24)
	obj := &Object{Kind: ObjVideo}
	s.Reserve(Rect{X: 10, Y: 4, W: 20, H: 12}, obj)

	ext := s.Extents()[0]
	row, col := ext.Visible.Y+1, ext.Visible.X+1
	if row != 5 || col != 11 {
		t.Errorf("player position = (row %d, col %d), want (5, 11)", row, col)
	}
}

// sampleFrame builds the frame shared by the golden test and -print. It mimics
// a real browsing screen: a status row of chained click targets, a reserved
// video, the username and music lines, a truncated caption, and a comments
// panel whose last GIF hangs off the bottom edge.
//
// Deliberately multilingual — comment authors and captions are arbitrary user
// text, and the double-width CJK and combining Arabic are exactly what shifts
// zone boundaries if the width handling is wrong.
func sampleFrame() *Screen {
	var (
		pink200   = lipgloss.NewStyle().Foreground(colors.Pink200Color)
		pink500   = lipgloss.NewStyle().Foreground(colors.Pink500Color)
		purple200 = lipgloss.NewStyle().Foreground(colors.Purple200Color)
		purple400 = lipgloss.NewStyle().Foreground(colors.Purple400Color)
		blue500   = lipgloss.NewStyle().Foreground(colors.Blue500Color)
		yellow500 = lipgloss.NewStyle().Foreground(colors.Yellow500Color)
		gray50    = lipgloss.NewStyle().Foreground(colors.Gray50Color)
		gray300   = lipgloss.NewStyle().Foreground(colors.Gray300Color)
		gray600   = lipgloss.NewStyle().Foreground(colors.Gray600Color)
	)

	s := New(48, 16)
	const left = 4
	row := func(y int) Rect { return Rect{X: left, Y: y, W: 40, H: 1} }

	// Status row: each icon and its count is one target, chained off the
	// previous run's cursor so nothing measures anything.
	x := left
	x, _ = s.SetContent(Rect{X: x, Y: 0, W: 44, H: 1},
		pink500.Render("❤️")+" 1.2K", &Zone{Owner: OwnerRoot, Target: 1})
	x, _ = s.SetContent(Rect{X: x, Y: 0, W: 44, H: 1},
		"   💬 45", &Zone{Owner: OwnerRoot, Target: 2})
	x, _ = s.SetContent(Rect{X: x, Y: 0, W: 44, H: 1},
		"   "+purple400.Render("⇄")+" 12", &Zone{Owner: OwnerRoot, Target: 3})
	s.SetContent(Rect{X: x, Y: 0, W: 44, H: 1},
		"   "+yellow500.Render("⚑"), &Zone{Owner: OwnerRoot, Target: 4})

	s.Reserve(Rect{X: left, Y: 1, W: 22, H: 7}, &Object{Kind: ObjVideo})

	s.SetContent(row(8), pink500.Bold(true).Render("@someone")+" "+blue500.Render("✓"), nil)
	s.SetContent(row(9), purple200.Italic(true).Render("ある曲 - アーティスト [E]"), nil)
	s.SetContent(row(10), gray300.Render("caption text that runs past the edge and is cut"), nil)

	s.SetContent(row(11), purple400.Bold(true).Underline(true).Render("Comments"), nil)

	// One zone per comment, shared across its author and body lines, so a click
	// on either row resolves to the same comment.
	first := &Zone{Owner: OwnerComments, Target: 0}
	s.SetContent(row(12), pink200.Bold(true).Render("@用户名"), first)
	s.SetContent(row(13), gray50.Render("  你好世界 مرحبا 안녕하세요 🇯🇵"), first)

	s.SetContent(row(14), pink200.Bold(true).Render("@مستخدم"), &Zone{Owner: OwnerComments, Target: 1})
	s.SetContent(Rect{X: left, Y: 15, W: 40, H: 1}, gray600.Render("  ↳ 3 replies"), nil)
	// Hangs off the bottom, so Extents reports it clipped.
	s.Reserve(Rect{X: 6, Y: 15, W: 10, H: 4}, &Object{Kind: ObjGif})

	return s
}

// dumpSections is the golden-file content: the three grid views, no styling.
func dumpSections(s *Screen) string {
	return "== Dump ==\n" + s.Dump() +
		"\n== Objects ==\n" + s.DumpObjects() +
		"\n== Zones ==\n" + s.DumpZones()
}

// Golden dump: the artifact for eyeballing what the matrix looks like.
//
//	go test ./tui/screen/ -run TestDumpGolden -print    # see the frame
//	go test ./tui/screen/ -update                       # rewrite the golden
func TestDumpGolden(t *testing.T) {
	s := sampleFrame()
	got := dumpSections(s)

	if *printDump {
		// Straight to stdout, not t.Log: the testing package indents every
		// logged line, which would wreck the column rulers. Render() last so
		// the styles actually show up in a terminal.
		fmt.Print("\n" + got)
		fmt.Print("\n== Rendered ==\n" + s.Render() + "\n")
	}

	golden := filepath.Join("testdata", "dump.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("%v (run `go test ./tui/screen/ -update` to create it)", err)
	}
	if got != string(want) {
		t.Errorf("dump mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
