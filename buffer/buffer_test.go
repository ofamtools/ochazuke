package buffer

import (
	"math"
	"strings"
	"testing"

	"github.com/rivo/uniseg"

	"ofam.dev/ochazuke/layout"
)

// assertGridInvariants verifies that every row of b has consistent lead/cont cells
// summing to b.w columns, with no dangling continuation cells or stale widths.
func assertGridInvariants(t *testing.T, b *Buffer) {
	t.Helper()
	if b == nil {
		return
	}
	if len(b.cells) != b.w*b.h {
		t.Fatalf("cells length = %d, want %d*%d=%d", len(b.cells), b.w, b.h, b.w*b.h)
	}
	for y := 0; y < b.h; y++ {
		rowOff := y * b.w
		x := 0
		for x < b.w {
			c := b.cells[rowOff+x]
			if c.cont {
				t.Fatalf("row %d col %d: unexpected dangling continuation cell", y, x)
			}
			if c.width < 1 {
				t.Fatalf("row %d col %d: lead cell width = %d, want >= 1", y, x, c.width)
			}
			if sw := uniseg.StringWidth(c.ch); sw != int(c.width) {
				t.Fatalf("row %d col %d: cell %q stored width=%d, uniseg.StringWidth=%d", y, x, c.ch, c.width, sw)
			}
			w := int(c.width)
			if x+w > b.w {
				t.Fatalf("row %d col %d: lead cell width %d overflows buffer width %d", y, x, w, b.w)
			}
			for k := 1; k < w; k++ {
				cc := b.cells[rowOff+x+k]
				if !cc.cont || cc.width != 0 || cc.ch != "" {
					t.Fatalf("row %d col %d+%d: invalid continuation cell %+v", y, x, k, cc)
				}
			}
			x += w
		}
	}
}

func TestNewAndDimensions(t *testing.T) {
	tests := []struct {
		name      string
		w, h      int
		wantW     int
		wantH     int
		wantStr   string
		wantShape CursorShape
	}{
		{
			name:    "normal 4x2",
			w:       4,
			h:       2,
			wantW:   4,
			wantH:   2,
			wantStr: "    \n    ",
		},
		{
			name:    "zero 0x0",
			w:       0,
			h:       0,
			wantW:   0,
			wantH:   0,
			wantStr: "",
		},
		{
			name:    "negative width",
			w:       -5,
			h:       3,
			wantW:   0,
			wantH:   3,
			wantStr: "",
		},
		{
			name:    "negative height",
			w:       5,
			h:       -2,
			wantW:   5,
			wantH:   0,
			wantStr: "",
		},
		{
			name:    "both negative",
			w:       -1,
			h:       -1,
			wantW:   0,
			wantH:   0,
			wantStr: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := New(tc.w, tc.h)
			if b.Width() != tc.wantW || b.Height() != tc.wantH {
				t.Fatalf("got %dx%d, want %dx%d", b.Width(), b.Height(), tc.wantW, tc.wantH)
			}
			if b.Cursor.Shape != tc.wantShape {
				t.Fatalf("Cursor.Shape = %v, want %v", b.Cursor.Shape, tc.wantShape)
			}
			if got := b.String(); got != tc.wantStr {
				t.Fatalf("String() = %q, want %q", got, tc.wantStr)
			}
			assertGridInvariants(t, b)
		})
	}

	shapes := map[CursorShape]bool{
		CursorHidden:    true,
		CursorBlock:     true,
		CursorBar:       true,
		CursorUnderline: true,
	}
	if len(shapes) != 4 {
		t.Fatalf("expected 4 distinct CursorShape values, got %d", len(shapes))
	}

	var nilBuf *Buffer
	if nilBuf.Width() != 0 || nilBuf.Height() != 0 || nilBuf.String() != "" {
		t.Fatalf("nil buffer should return zero dims and empty string")
	}
}

func TestSetLineBoundsAndOverwrite(t *testing.T) {
	b := New(6, 2)
	b.SetLine(-1, "ignored")
	b.SetLine(2, "ignored")
	if got := b.String(); got != "      \n      " {
		t.Fatalf("out-of-bounds SetLine modified buffer: %q", got)
	}

	b.SetLine(0, "\x1b[31mabcdef\x1b[0m")
	if got := b.String(); got != "\x1b[31mabcdef\x1b[0m\n      " {
		t.Fatalf("unexpected row 0: %q", got)
	}

	// Shorter overwrite clears stale characters and styles.
	b.SetLine(0, "xy")
	if got := b.String(); got != "xy    \n      " {
		t.Fatalf("shorter overwrite left stale data: %q", got)
	}

	// Empty overwrite clears row completely.
	b.SetLine(0, "")
	if got := b.String(); got != "      \n      " {
		t.Fatalf("empty overwrite left stale data: %q", got)
	}
	assertGridInvariants(t, b)
}

func TestSetLineGraphemesAndWidthClipping(t *testing.T) {
	tests := []struct {
		name string
		w    int
		text string
		want string
	}{
		{
			name: "ascii exact",
			w:    4,
			text: "abcd",
			want: "abcd",
		},
		{
			name: "ascii overflow",
			w:    4,
			text: "abcdef",
			want: "abcd",
		},
		{
			name: "cjk odd width clip",
			w:    5,
			text: "日本語",
			want: "日本 ",
		},
		{
			name: "cjk even width fit",
			w:    4,
			text: "日本語",
			want: "日本",
		},
		{
			name: "mixed ascii and wide clip",
			w:    2,
			text: "a日b",
			want: "a ",
		},
		{
			name: "combining mark at boundary",
			w:    2,
			text: "ab\u0301c",
			want: "ab\u0301",
		},
		{
			name: "combining mark split by SGR",
			w:    3,
			text: "e\x1b[31m\u0301x",
			want: "e\u0301\x1b[31mx\x1b[0m ",
		},
		{
			name: "leading orphan combining mark skipped",
			w:    3,
			text: "\u0301ab",
			want: "ab ",
		},
		{
			name: "zwj emoji fits w=2",
			w:    2,
			text: "👨‍👩‍👧",
			want: "👨‍👩‍👧",
		},
		{
			name: "zwj emoji clipped in w=1",
			w:    1,
			text: "👨‍👩‍👧",
			want: " ",
		},
		{
			name: "vs16 emoji width 2 clipped in w=1",
			w:    1,
			text: "❤️",
			want: " ",
		},
		{
			name: "vs16 emoji fits in w=2",
			w:    2,
			text: "❤️",
			want: "❤️",
		},
		{
			name: "plain heart width 1 fits in w=1",
			w:    1,
			text: "❤",
			want: "❤",
		},
		{
			name: "regional indicator flag width 2",
			w:    3,
			text: "🇺🇸a",
			want: "🇺🇸a",
		},
		{
			name: "two-em dash width 3 fits",
			w:    3,
			text: "\u2e3a",
			want: "\u2e3a",
		},
		{
			name: "two-em dash width 3 clipped in w=2",
			w:    2,
			text: "\u2e3a",
			want: "  ",
		},
		{
			name: "two-em dash after char clipped in w=3",
			w:    3,
			text: "a\u2e3a",
			want: "a  ",
		},
		{
			name: "three-em dash width 4 fits",
			w:    4,
			text: "\u2e3b",
			want: "\u2e3b",
		},
		{
			name: "three-em dash width 4 clipped in w=3",
			w:    3,
			text: "\u2e3b",
			want: "   ",
		},
		{
			name: "tab expansion 4-col stops",
			w:    8,
			text: "a\tb",
			want: "a   b   ",
		},
		{
			name: "tab at column 4 fills to column 8",
			w:    8,
			text: "abcd\tx",
			want: "abcd    ",
		},
		{
			name: "tab at column 3 advances 1 space to stop 4",
			w:    8,
			text: "abc\tx",
			want: "abc x   ",
		},
		{
			name: "tab clipped at end",
			w:    3,
			text: "a\tb",
			want: "a  ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := New(tc.w, 1)
			b.SetLine(0, tc.text)
			if got := b.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			assertGridInvariants(t, b)
		})
	}
}

func TestSetLineSGROSC8AndSanitization(t *testing.T) {
	tests := []struct {
		name string
		w    int
		text string
		want string
	}{
		{
			name: "truecolor black 0 params do not reset bold",
			w:    2,
			text: "\x1b[1;38;2;0;0;0mhi\x1b[0m",
			want: "\x1b[1;38;2;0;0;0mhi\x1b[0m",
		},
		{
			name: "v1 regression 38;2;0;39;0 then 39;1",
			w:    11,
			text: "\x1b[0;38;2;0;39;0mgreen\x1b[39;1mnormal\x1b[m",
			want: "\x1b[38;2;0;39;0mgreen\x1b[0m\x1b[1mnormal\x1b[0m",
		},
		{
			name: "256 color index 0 preserves underline",
			w:    2,
			text: "\x1b[4;38;5;0mhi\x1b[0m",
			want: "\x1b[4;38;5;0mhi\x1b[0m",
		},
		{
			name: "48;2 and 58;2 with 0 channels",
			w:    2,
			text: "\x1b[1;48;2;0;0;0;58;2;0;0;0mhi\x1b[0m",
			want: "\x1b[1;48;2;0;0;0;58;2;0;0;0mhi\x1b[0m",
		},
		{
			name: "colon extended color and underline",
			w:    2,
			text: "\x1b[1;4:3;38:2::0:0:0;48:5:0mhi\x1b[4:0m",
			want: "\x1b[1;4:3;38:2::0:0:0;48:5:0mhi\x1b[0m",
		},
		{
			name: "truncated extended color does not trigger reset",
			w:    2,
			text: "\x1b[1;38;2;0mhi\x1b[0m",
			want: "\x1b[1mhi\x1b[0m",
		},
		{
			name: "shorthand reset and mid-param reset",
			w:    2,
			text: "\x1b[31;0;32mhi\x1b[m",
			want: "\x1b[32mhi\x1b[0m",
		},
		{
			name: "cumulative SGR and partial reset 22",
			w:    3,
			text: "\x1b[1m\x1b[31ma\x1b[22mb\x1b[0mc",
			want: "\x1b[1;31ma\x1b[0m\x1b[31mb\x1b[0mc",
		},
		{
			name: "adjacent identical SGR coalesced",
			w:    2,
			text: "\x1b[31ma\x1b[31mb\x1b[0m",
			want: "\x1b[31mab\x1b[0m",
		},
		{
			name: "unclosed SGR closed before trailing spaces",
			w:    4,
			text: "\x1b[31mhi",
			want: "\x1b[31mhi\x1b[0m  ",
		},
		{
			name: "unclosed SGR closed at EOL",
			w:    2,
			text: "\x1b[31mhi",
			want: "\x1b[31mhi\x1b[0m",
		},
		{
			name: "OSC 8 BEL form",
			w:    6,
			text: "\x1b]8;id=1;https://example.com\x07link\x1b]8;;\x07",
			want: "\x1b]8;id=1;https://example.com\x07link\x1b]8;;\x07  ",
		},
		{
			name: "OSC 8 ST form and SGR reset keeps link active",
			w:    4,
			text: "\x1b]8;;https://example.com\x1b\\\x1b[31ma\x1b[0mb",
			want: "\x1b]8;;https://example.com\x07\x1b[31ma\x1b[0mb\x1b]8;;\x07  ",
		},
		{
			name: "OSC 8 link change emits close before new link",
			w:    2,
			text: "\x1b]8;;https://a\x07a\x1b]8;;https://b\x07b",
			want: "\x1b]8;;https://a\x07a\x1b]8;;\x07\x1b]8;;https://b\x07b\x1b]8;;\x07",
		},
		{
			name: "non-SGR CSI and non-8 OSC and DCS and nF stripped",
			w:    6,
			text: "\x1b[2K\x1b[?25l\x1b[>4;2m\x1b]0;title\x07\x1b]2;t2\x1b\\\x1bPdcs\x1b\\\x1b(B\x1b7ok",
			want: "ok    ",
		},
		{
			name: "incomplete trailing escapes safe",
			w:    4,
			text: "ab\x1b[31",
			want: "ab  ",
		},
		{
			name: "incomplete lone ESC and unclosed OSC safe",
			w:    4,
			text: "ab\x1b]8;;unclosed",
			want: "ab  ",
		},
		{
			name: "control chars C0 C1 separators sanitized",
			w:    4,
			text: "a\r\n\x00\x7f\u0085\u2028\u2029b",
			want: "ab  ",
		},
		{
			name: "invalid UTF-8 replaced by U+FFFD",
			w:    3,
			text: "a\xffb",
			want: "a\ufffdb",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := New(tc.w, 1)
			b.SetLine(0, tc.text)
			if got := b.String(); got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
			assertGridInvariants(t, b)
		})
	}
}

func TestStringMultiRowNoTrailingNewlineAndNoBleed(t *testing.T) {
	b := New(4, 2)
	b.SetLine(0, "\x1b[31m\x1b]8;;https://a\x07abcd")
	b.SetLine(1, "next")
	got := b.String()
	want := "\x1b]8;;https://a\x07\x1b[31mabcd\x1b]8;;\x07\x1b[0m\nnext"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.HasSuffix(got, "\n") {
		t.Fatalf("String() should not have trailing newline: %q", got)
	}
	assertGridInvariants(t, b)
}

func TestFill(t *testing.T) {
	t.Run("basic and ch==0 and styleANSI variants", func(t *testing.T) {
		b := New(5, 2)
		b.Fill(layout.Rect{X: 1, Y: 0, W: 3, H: 1}, 'x', "31")
		b.Fill(layout.Rect{X: 0, Y: 1, W: 5, H: 1}, 0, "\x1b[1;38;2;0;0;0m")
		want := " \x1b[31mxxx\x1b[0m \n\x1b[1;38;2;0;0;0m     \x1b[0m"
		if got := b.String(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		// Reset style with "\x1b[0m"
		b.Fill(layout.Rect{X: 0, Y: 1, W: 5, H: 1}, ' ', "\x1b[0m")
		if got := b.String(); got != " \x1b[31mxxx\x1b[0m \n     " {
			t.Fatalf("reset Fill failed: %q", got)
		}
		assertGridInvariants(t, b)
	})

	t.Run("wide rune fill and clipping", func(t *testing.T) {
		b := New(5, 1)
		b.Fill(layout.Rect{X: 0, Y: 0, W: 5, H: 1}, '日', "")
		if got := b.String(); got != "日日 " {
			t.Fatalf("got %q, want %q", got, "日日 ")
		}

		b1 := New(1, 1)
		b1.Fill(layout.Rect{X: 0, Y: 0, W: 1, H: 1}, '日', "")
		if got := b1.String(); got != " " {
			t.Fatalf("w=1 wide Fill got %q, want %q", got, " ")
		}

		b4 := New(4, 1)
		b4.Fill(layout.Rect{X: 0, Y: 0, W: 4, H: 1}, '\u2e3a', "")
		if got := b4.String(); got != "\u2e3a " {
			t.Fatalf("width-3 Fill got %q, want %q", got, "\u2e3a ")
		}
		assertGridInvariants(t, b)
		assertGridInvariants(t, b1)
		assertGridInvariants(t, b4)
	})

	t.Run("split wide-char edge repair", func(t *testing.T) {
		// Width-2 CJK cells at 0..5, overwrite [1..5)
		b := New(6, 1)
		b.SetLine(0, "日本語")
		b.Fill(layout.Rect{X: 1, Y: 0, W: 4, H: 1}, 'x', "")
		if got := b.String(); got != " xxxx " {
			t.Fatalf("width-2 repair got %q, want %q", got, " xxxx ")
		}
		assertGridInvariants(t, b)

		// Width-3 U+2E3A at 0..2, overwrite [1..2)
		b3 := New(3, 1)
		b3.SetLine(0, "\u2e3a")
		b3.Fill(layout.Rect{X: 1, Y: 0, W: 1, H: 1}, 'x', "")
		if got := b3.String(); got != " x " {
			t.Fatalf("width-3 repair got %q, want %q", got, " x ")
		}
		assertGridInvariants(t, b3)

		// Width-4 U+2E3B at 0..3, overwrite [2..3)
		b4 := New(4, 1)
		b4.SetLine(0, "\u2e3b")
		b4.Fill(layout.Rect{X: 2, Y: 0, W: 1, H: 1}, 'x', "")
		if got := b4.String(); got != "  x " {
			t.Fatalf("width-4 repair got %q, want %q", got, "  x ")
		}
		assertGridInvariants(t, b4)
	})

	t.Run("offscreen and negative rects safe", func(t *testing.T) {
		b := New(4, 2)
		b.SetLine(0, "abcd")
		b.Fill(layout.Rect{X: -10, Y: -5, W: 3, H: 2}, 'z', "31")
		b.Fill(layout.Rect{X: 10, Y: 0, W: 2, H: 2}, 'z', "31")
		b.Fill(layout.Rect{X: 0, Y: 0, W: 0, H: 2}, 'z', "31")
		b.Fill(layout.Rect{X: -2, Y: 0, W: 4, H: 1}, 'z', "")
		if got := b.String(); got != "zzcd\n    " {
			t.Fatalf("got %q, want %q", got, "zzcd\n    ")
		}
		assertGridInvariants(t, b)
	})
}

func TestBlitGeometryRepairAndCursor(t *testing.T) {
	t.Run("nil src safe", func(t *testing.T) {
		b := New(3, 1)
		b.SetLine(0, "abc")
		b.Blit(nil, layout.Rect{X: 0, Y: 0, W: 3, H: 1})
		if got := b.String(); got != "abc" {
			t.Fatalf("got %q, want %q", got, "abc")
		}
	})

	t.Run("clipped src and dest wide-char repair", func(t *testing.T) {
		dst := New(6, 1)
		dst.SetLine(0, "日本語")

		src := New(4, 1)
		src.SetLine(0, "\x1b[32mab\x1b[0m")

		dst.Blit(src, layout.Rect{X: 1, Y: 0, W: 4, H: 1})
		// cols 0 (lead of 日) and 5 (cont of 語) repaired to ' ', cols 1..2 green ab, cols 3..4 spaces
		want := " \x1b[32mab\x1b[0m   "
		if got := dst.String(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		assertGridInvariants(t, dst)
	})

	t.Run("src wide-char clipped on left and right", func(t *testing.T) {
		src := New(4, 1)
		src.SetLine(0, "日本")

		// Left clip: at.X = -1 cuts lead of '日', leaving cont -> repaired to ' '
		dstL := New(4, 1)
		dstL.Blit(src, layout.Rect{X: -1, Y: 0, W: 4, H: 1})
		if got := dstL.String(); got != " 本 " {
			t.Fatalf("left clip got %q, want %q", got, " 本 ")
		}
		assertGridInvariants(t, dstL)

		// Right clip: at.W = 3 cuts cont of '本', leaving lead -> repaired to ' '
		dstR := New(4, 1)
		dstR.Blit(src, layout.Rect{X: 0, Y: 0, W: 3, H: 1})
		if got := dstR.String(); got != "日  " {
			t.Fatalf("right clip got %q, want %q", got, "日  ")
		}
		assertGridInvariants(t, dstR)

		// Width-4 U+2E3B clipped on left and right
		src4 := New(6, 1)
		src4.SetLine(0, "\u2e3bxy")
		dst4L := New(6, 1)
		dst4L.Blit(src4, layout.Rect{X: -2, Y: 0, W: 6, H: 1})
		if got := dst4L.String(); got != "  xy  " {
			t.Fatalf("width-4 left clip got %q, want %q", got, "  xy  ")
		}
		assertGridInvariants(t, dst4L)

		dst4R := New(6, 1)
		dst4R.Blit(src4, layout.Rect{X: 0, Y: 0, W: 3, H: 1})
		if got := dst4R.String(); got != "      " {
			t.Fatalf("width-4 right clip got %q, want %q", got, "      ")
		}
		assertGridInvariants(t, dst4R)
	})

	t.Run("self blit aliasing safe", func(t *testing.T) {
		b := New(6, 1)
		b.SetLine(0, "abcdef")
		b.Blit(b, layout.Rect{X: 2, Y: 0, W: 4, H: 1})
		if got := b.String(); got != "ababab" && got != "ababcd" {
			// Wait: src snapshot is "abcdef", copied into X=2..6 (W=4) -> "ababcd"!
			t.Fatalf("self blit got %q, want %q", got, "ababcd")
		}
		if got := b.String(); got != "ababcd" {
			t.Fatalf("self blit got %q, want %q", got, "ababcd")
		}
		assertGridInvariants(t, b)
	})

	t.Run("cursor translation and clipping", func(t *testing.T) {
		dst := New(10, 5)
		dst.Cursor = Cursor{X: 1, Y: 1, Shape: CursorBar}

		// Hidden src cursor leaves dst cursor untouched
		srcHidden := New(3, 2)
		srcHidden.Cursor = Cursor{X: 0, Y: 0, Shape: CursorHidden}
		dst.Blit(srcHidden, layout.Rect{X: 4, Y: 2, W: 3, H: 2})
		if dst.Cursor != (Cursor{X: 1, Y: 1, Shape: CursorBar}) {
			t.Fatalf("hidden src cursor overwrote dst cursor: %+v", dst.Cursor)
		}

		// Visible src cursor inside clipped bounds translates
		srcVis := New(4, 3)
		srcVis.Cursor = Cursor{X: 2, Y: 1, Shape: CursorUnderline}
		dst.Blit(srcVis, layout.Rect{X: 3, Y: 2, W: 4, H: 3})
		if dst.Cursor != (Cursor{X: 5, Y: 3, Shape: CursorUnderline}) {
			t.Fatalf("translated cursor = %+v, want {5, 3, CursorUnderline}", dst.Cursor)
		}

		// Visible src cursor clipped by at.W drops cursor
		dst.Blit(srcVis, layout.Rect{X: 3, Y: 2, W: 2, H: 3})
		if dst.Cursor.Shape != CursorHidden {
			t.Fatalf("cursor clipped by at.W should drop to CursorHidden, got %+v", dst.Cursor)
		}

		// Visible src cursor outside dst bounds drops cursor
		srcVis.Cursor = Cursor{X: 3, Y: 2, Shape: CursorBlock}
		dst.Blit(srcVis, layout.Rect{X: 8, Y: 2, W: 4, H: 3})
		if dst.Cursor.Shape != CursorHidden {
			t.Fatalf("cursor outside dst should drop to CursorHidden, got %+v", dst.Cursor)
		}
	})

	t.Run("styles and OSC 8 preserved across Blit", func(t *testing.T) {
		src := New(4, 1)
		src.SetLine(0, "\x1b[1;34m\x1b]8;;https://go.dev\x07go\x1b]8;;\x07\x1b[0m")
		dst := New(6, 1)
		dst.Blit(src, layout.Rect{X: 2, Y: 0, W: 4, H: 1})
		want := "  \x1b]8;;https://go.dev\x07\x1b[1;34mgo\x1b]8;;\x07\x1b[0m  "
		if got := dst.String(); got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
		assertGridInvariants(t, dst)
	})
}

func stripANSITest(s string) string {
	var sb strings.Builder
	for i := 0; i < len(s); {
		if s[i] == '\x1b' {
			var sgr sgrState
			var link string
			i = parseEscape(s, i, &sgr, &link)
			continue
		}
		sb.WriteByte(s[i])
		i++
	}
	return sb.String()
}

func TestRegressionCrossEscapeGraphemesOSC8AndOverflow(t *testing.T) {
	t.Run("cross-escape and control grapheme segmentation preserves width and idempotence", func(t *testing.T) {
		tests := []struct {
			name string
			w    int
			text string
			want string
		}{
			{
				name: "heart_SGR_VS16",
				w:    4,
				text: "\u2764\x1b[31m\ufe0f",
				want: "\u2764\ufe0f  ",
			},
			{
				name: "heart_LF_VS16",
				w:    4,
				text: "\u2764\n\ufe0f",
				want: "\u2764\ufe0f  ",
			},
			{
				name: "heart_SGR_VS16_clipped_w1",
				w:    1,
				text: "\u2764\x1b[31m\ufe0f",
				want: " ",
			},
			{
				name: "zwj_split_by_SGR",
				w:    6,
				text: "👩\x1b[31m\u200d🔬",
				want: "👩\u200d🔬    ",
			},
			{
				name: "flag_with_SGR_between_RI",
				w:    4,
				text: "\U0001F1FA\x1b[31m\U0001F1F8",
				want: "\U0001F1FA\U0001F1F8  ",
			},
			{
				name: "combining_mark_at_right_margin_across_SGR",
				w:    2,
				text: "ab\x1b[31m\u0301",
				want: "ab\u0301",
			},
			{
				name: "iter41_op5_cross_OSC8_SGR_ZWJ",
				w:    25,
				text: "👩\u200d🔬⃣\u200d\x1b]8;id=1;https://a.example;x=1\a\x1b[58;2;0;0;0m❤a",
				want: "👩\u200d🔬⃣\u200d❤\x1b]8;id=1;https://a.example;x=1\x07\x1b[58;2;0;0;0ma\x1b]8;;\x07\x1b[0m" + strings.Repeat(" ", 25-uniseg.StringWidth("👩\u200d🔬⃣\u200d❤a")),
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				b := New(tc.w, 1)
				b.SetLine(0, tc.text)
				got := b.String()
				if got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
				assertGridInvariants(t, b)
				if sw := uniseg.StringWidth(stripANSITest(got)); sw != tc.w {
					t.Fatalf("stripped display width = %d, want %d (got=%q)", sw, tc.w, got)
				}

				b2 := New(tc.w, 1)
				b2.SetLine(0, got)
				if got2 := b2.String(); got2 != got {
					t.Fatalf("SetLine not idempotent: first=%q second=%q", got, got2)
				}
				assertGridInvariants(t, b2)
			})
		}
	})

	t.Run("OSC 8 strips C1 controls line separators and invalid UTF-8", func(t *testing.T) {
		tests := []struct {
			name string
			w    int
			text string
			want string
		}{
			{
				name: "c1_csi_in_uri",
				w:    10,
				text: "\x1b]8;id=1;https://a.example/\u009b31m\x07link\x1b]8;;\x07",
				want: "\x1b]8;id=1;https://a.example/31m\x07link\x1b]8;;\x07      ",
			},
			{
				name: "c1_st_in_params",
				w:    10,
				text: "\x1b]8;id=\u009c;https://a.example\x07link\x1b]8;;\x07",
				want: "\x1b]8;id=;https://a.example\x07link\x1b]8;;\x07      ",
			},
			{
				name: "u2028_u2029_in_uri",
				w:    10,
				text: "\x1b]8;;https://a.example/\u2028\u2029foo\x07link\x1b]8;;\x07",
				want: "\x1b]8;;https://a.example/foo\x07link\x1b]8;;\x07      ",
			},
			{
				name: "invalid_utf8_in_uri",
				w:    10,
				text: "\x1b]8;;https://a.example/\xff\xfe\x07link\x1b]8;;\x07",
				want: "\x1b]8;;https://a.example/\x07link\x1b]8;;\x07      ",
			},
			{
				name: "uri_only_forbidden_closes_link",
				w:    6,
				text: "\x1b]8;id=1;https://a.example\x07a\x1b]8;id=2;\u009b\xff\x07b",
				want: "\x1b]8;id=1;https://a.example\x07a\x1b]8;;\x07b    ",
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				b := New(tc.w, 1)
				b.SetLine(0, tc.text)
				if got := b.String(); got != tc.want {
					t.Fatalf("got %q, want %q", got, tc.want)
				}
				assertGridInvariants(t, b)
			})
		}

		bf := New(4, 1)
		bf.Fill(layout.Rect{X: 0, Y: 0, W: 4, H: 1}, 'A', "\x1b]8;p=\u009b;https://a.example/\u009c\u2028\xff\x07")
		wantFill := "\x1b]8;p=;https://a.example/\x07AAAA\x1b]8;;\x07"
		if got := bf.String(); got != wantFill {
			t.Fatalf("Fill OSC 8 sanitization got %q, want %q", got, wantFill)
		}
		assertGridInvariants(t, bf)
	})

	t.Run("Fill and Blit handle math.MaxInt and math.MinInt without overflow", func(t *testing.T) {
		b := New(4, 2)
		b.Fill(layout.Rect{X: 1, Y: 0, W: math.MaxInt, H: 1}, 'X', "31")
		if got := b.String(); got != " \x1b[31mXXX\x1b[0m\n    " {
			t.Fatalf("Fill X=1 W=MaxInt got %q", got)
		}
		b.Fill(layout.Rect{X: 0, Y: 1, W: 4, H: math.MaxInt}, 'H', "")
		if got := b.String(); got != " \x1b[31mXXX\x1b[0m\nHHHH" {
			t.Fatalf("Fill Y=1 H=MaxInt got %q", got)
		}
		b.Fill(layout.Rect{X: math.MinInt, Y: 0, W: math.MaxInt, H: 1}, 'Z', "")
		if got := b.String(); got != " \x1b[31mXXX\x1b[0m\nHHHH" {
			t.Fatalf("Fill X=MinInt W=MaxInt modified buffer: %q", got)
		}
		assertGridInvariants(t, b)

		src := New(4, 2)
		src.SetLine(0, "ABCD")
		src.SetLine(1, "EFGH")
		src.Cursor = Cursor{X: 1, Y: 0, Shape: CursorBlock}

		dst := New(4, 2)
		dst.Blit(src, layout.Rect{X: 1, Y: 0, W: math.MaxInt, H: math.MaxInt})
		if got := dst.String(); got != " ABC\n EFG" {
			t.Fatalf("Blit X=1 W=MaxInt H=MaxInt got %q", got)
		}
		if dst.Cursor != (Cursor{X: 2, Y: 0, Shape: CursorBlock}) {
			t.Fatalf("Blit cursor = %+v, want {2, 0, CursorBlock}", dst.Cursor)
		}

		src.Cursor = Cursor{X: 3, Y: 0, Shape: CursorBar}
		dst.Blit(src, layout.Rect{X: math.MaxInt - 1, Y: 0, W: 4, H: 2})
		if dst.Cursor.Shape != CursorHidden {
			t.Fatalf("Blit overflowing cursor should drop to CursorHidden, got %+v", dst.Cursor)
		}
		assertGridInvariants(t, dst)
	})
}
