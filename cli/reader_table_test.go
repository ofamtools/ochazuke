package cli

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"ofam.dev/ochazuke"
	"ofam.dev/ochazuke/internal/readertest"
	"ofam.dev/ochazuke/internal/testenv"
)

// readerRuntime is readertest's fixture on a runtime: each run recorded
// in ran as "noun.action k=v,...".
func readerRuntime(t *testing.T, ran *[]string) *ochazuke.Runtime {
	t.Helper()
	testenv.ClearActor(t)
	r := ochazuke.NewRuntime()
	for _, o := range readertest.Ops {
		params, positional := map[string]ochazuke.Parameter{}, []string(nil)
		for _, p := range o.Params {
			params[p.Name] = ochazuke.Parameter{Type: p.Type, Enum: p.Enum}
			if p.Positional {
				positional = append(positional, p.Name)
			}
		}
		key := ochazuke.KeyOf(o.Noun, o.Action)
		r.Register(ochazuke.Operation{Key: key, Parameters: params, Positional: positional, Run: func(_ *ochazuke.Runtime, _ ochazuke.Context, a ochazuke.Arguments) (any, error) {
			*ran = append(*ran, key.Short()+" "+argsText(a))
			return "ok", nil
		}})
		if o.Alias != "" {
			if err := r.AddActionAlias(ochazuke.ActionAlias{Alias: o.Noun + "." + o.Alias, Action: key.Short()}); err != nil {
				t.Fatal(err)
			}
		}
	}
	r.Nouns[readertest.MainNoun].Main = true
	return r
}

func argsText(a ochazuke.Arguments) string {
	var out []string
	for k, v := range a {
		out = append(out, k+"="+fmt.Sprint(v))
	}
	slices.Sort(out)
	return strings.Join(out, ",")
}

// The CLI reads readertest's lines as the palette does (the root package's
// TestReaderTablePalette runs the same rows), and its complete lists the
// same words.
func TestReaderTable(t *testing.T) {
	for _, row := range readertest.Rows {
		var ran []string
		r := readerRuntime(t, &ran)
		_, errOut, err := oXNamed(t, "o-fixture", r, strings.Fields(row.Line)...)
		got, args := "", ""
		if len(ran) > 0 {
			got, args, _ = strings.Cut(ran[0], " ")
		}
		switch {
		case row.Err != "":
			if err == nil || !strings.Contains(errOut+errText(err), row.Err) {
				t.Errorf("%q: ran %v err %v, want an error holding %q\n%s", row.Line, ran, err, row.Err, errOut)
			}
		case err != nil || got != row.Op || args != row.Args:
			t.Errorf("%q: ran %q args %q err %v, want %s %q\n%s", row.Line, got, args, err, row.Op, row.Args, errOut)
		}
	}
	for _, tc := range readertest.Completions {
		var ran []string
		sh := New(readerRuntime(t, &ran), "o-fixture", strings.NewReader(""), &strings.Builder{}, &strings.Builder{})
		var got []string
		for _, ch := range sh.completeWords(tc.Words) {
			got = append(got, ch.ID)
		}
		if !slices.Equal(got, tc.Want) {
			t.Errorf("complete %q = %v, want %v", tc.Words, got, tc.Want)
		}
	}
}
