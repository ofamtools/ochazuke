package lexiconcheck

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// WordRow is one row of the word list: a word's part of speech, whether the
// owner reviewed it and a note.
type WordRow struct {
	Word     string
	Part     string // noun, action, prop, relation, filter, namespace, joined with +
	Reviewed string // seed or yes
	Note     string // "singular": looks plural, is not
}

// Has reports whether the word has the part of speech.
func (r WordRow) Has(part string) bool {
	for _, p := range strings.Split(r.Part, "+") {
		if p == part {
			return true
		}
	}
	return false
}

// Only reports whether the part is the word's one part of speech.
func (r WordRow) Only(part string) bool { return r.Part == part }

// Words is the word list by word (the shared dictionary,
// ~/hub/tools/lexicon-audit/words.tsv).
type Words map[string]WordRow

// Singular reports whether the list marks w as looking plural but singular.
func (w Words) Singular(word string) bool { return w != nil && w[word].Note == "singular" }

// ParseWords reads the tab-separated text of a word list; # lines and blank
// lines are skipped. A duplicate word is an error.
func ParseWords(text string) (Words, error) {
	out := Words{}
	sc := bufio.NewScanner(strings.NewReader(text))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) < 3 || len(f) > 4 || f[0] == "" || f[1] == "" {
			return nil, fmt.Errorf("words line %d: want word, part, reviewed[, note]", n)
		}
		r := WordRow{Word: f[0], Part: f[1], Reviewed: f[2]}
		if len(f) == 4 {
			r.Note = f[3]
		}
		if _, dup := out[r.Word]; dup {
			return nil, fmt.Errorf("words line %d: duplicate word %q", n, r.Word)
		}
		out[r.Word] = r
	}
	return out, sc.Err()
}

// LoadWords reads a word list file.
func LoadWords(path string) (Words, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return ParseWords(string(b))
}
