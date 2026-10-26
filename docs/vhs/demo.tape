# The README demo: the CSV example over 3,000 rows of fake orders.
# Regenerate from the repository root:  vhs docs/vhs/demo.tape

Output assets/demo.gif

Require go

Set Shell "bash"
Set Theme "Catppuccin Mocha"
Set FontSize 16
Set Width 960
Set Height 560
Set Padding 16
Set WindowBar Colorful
Set Framerate 15
Set TypingSpeed 70ms
Set CursorBlink false

# Build off camera and open the sheet by a short relative path.
Hide
Type "export OZ=$(mktemp -d) && GOWORK=off go build -o $OZ/csv ./examples/csv && cd docs/vhs/data && clear && $OZ/csv orders.csv"
Enter
Sleep 4s
Show
Sleep 1.2s

# Move with counts, jump to the bottom and back.
Type "12j"
Sleep 600ms
Type "G"
Sleep 900ms
Type "gg"
Sleep 700ms

# The palette runs a verb on the whole customer column, then undo.
Type "2l"
Sleep 400ms
Type ":"
Sleep 400ms
Type "transform column upper"
Sleep 500ms
Enter
Sleep 1.3s
Type "u"
Sleep 900ms

# Filter by city, then sort the sheet by date.
Type "2l"
Type "/"
Sleep 300ms
Type "Osaka"
Enter
Sleep 1.2s
Type "3h"
Sleep 300ms
Type "s"
Sleep 1.3s

# Edit a status cell in place, then undo it.
Type "$"
Sleep 400ms
Type "i"
Sleep 300ms
Ctrl+U
Type "on hold"
Enter
Sleep 1s
Type "u"
Sleep 1.5s

Hide
Ctrl+C
