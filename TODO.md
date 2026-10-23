
# TODO

- [x] Cursor should not move and reposition text after cursor.
    - `InputView` now inverts the existing grapheme instead of inserting `▏`.
    - Empty/end positions use a styled blank cell; wide/combining characters,
      clipping, and visual selections are covered by `cursor_view_test.go`.
    - Verified with the full suite and an actual terminal: moving across
      `abcdef` changes the highlighted cell without shifting the text.
    - How Bubble Tea/Bubbles does it: Bubble Tea hides the native terminal
      cursor; Bubbles text inputs give the character under the cursor to
      `Cursor.SetChar`, render that character through `Cursor.View`, and then
      render the suffix starting at the next character. `Cursor.View` uses
      Lip Gloss `Reverse(true)` for its visible cursor phase. At the end it
      uses a space. Sources: [Bubbles cursor](https://github.com/charmbracelet/bubbles/blob/v0.21.0/cursor/cursor.go#L208-L218),
      [Bubbles textinput](https://github.com/charmbracelet/bubbles/blob/v0.21.0/textinput/textinput.go#L659-L683),
      [Bubble Tea terminal initialization](https://github.com/charmbracelet/bubbletea/blob/v1.3.10/tty.go#L35).

