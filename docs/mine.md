
# How I would structure things

Component {
  x,y
  x2,y2  // could be methods that return x + w and y + h
         // no harm storing the ints tho
  w,h
  focusable
  focusMode
  tabIndex
  parentComponent
}

Pixel {
  content
  visible  // ctrl/color codes are false
  affectsStyle
  beforeStyle
  afterStyle
  affectsPosition
  beforePosition
  afterPosition
}

// Alternative: nil means the pixel does not affect that state.
Pixel {
  content
  visible  // ctrl/color codes are false
  Style     *Change[Style]
  Position  *Change[Position]
}

Change[T] {
  Before T
  After  T
}

// Complete terminal style state, not a partial update.
// Store by value so Change[Style] snapshots remain independent.
Style {
  Foreground     Color
  Background     Color
  UnderlineColor Color
  Bold           bool
  Faint          bool  // independent of Bold
  Italic         bool
  Underline      enum { None, Single, Double, Curly, Dotted, Dashed }
  Blink          enum { None, Slow, Rapid }
  Reverse        bool  // swap displayed foreground and background
  Conceal        bool  // hide glyphs without removing their occupied cells
  Strikethrough  bool
  Overline       bool
}

Color {
  Kind   enum { Default, Indexed, RGB }
  Index  uint8  // used only for Indexed: terminal palette entry 0–255
  R,G,B  uint8  // used only for RGB
}

// Zero values mean terminal-default colors and no attributes.
// Default is not black or transparent. UnderlineColor Default follows Foreground.
// Keep unused Color fields zero so equal colors compare equal.
// Pixel.visible identifies printable content; Style.Conceal is terminal styling.

Affected {
  type // enum
  beforeValue
  afterValue
}

Page {
  retainState // if switching to another page and back do we stay where we were?
}

