package ochazuke

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// tickerFixture is a model over a page with one owner instance, the
// application verbs thing.build and thing.ship, the quiet thing.reload,
// and a clock the test moves.
func tickerFixture(t *testing.T) (*Runtime, *Model, *time.Time) {
	t.Helper()
	r := NewRuntime()
	noop := func(Ctx) Result { return Done(nil) }
	r.Define("thing").
		Verb("build", noop, "Build the thing").
		Verb("ship", noop, "Ship the thing").
		Verb("reload", noop, "Reload the things").Quiet()
	if _, err := r.AddPage("main", "Main", func(*Runtime, int, int) Block { return Text("body") }); err != nil {
		t.Fatal(err)
	}
	r.Instance("owner", Parent("main"), Kind("thing"))
	m := NewModel(r)
	m.Width, m.Height = 80, 24
	now := time.Unix(1000, 0)
	m.now = func() time.Time { return now }
	return r, m, &now
}

func issue(t *testing.T, r *Runtime, verb string) Ticket {
	t.Helper()
	ticket, _, err := r.Issue("owner", verb, "", Ctx{})
	if err != nil {
		t.Fatal(err)
	}
	return ticket
}

// frameMsg is any message: each Update is a frame the ticker observes.
var frameMsg = tea.WindowSizeMsg{Width: 80, Height: 24}

func tickerIs(t *testing.T, m *Model, want ...string) {
	t.Helper()
	m.Update(frameMsg)
	if got := m.JobsTicker(80); !reflect.DeepEqual(got, want) && !(len(got) == 0 && len(want) == 0) {
		t.Fatalf("ticker = %q, want %q", got, want)
	}
}

func TestJobsTickerRunningOnTopFinishedSlidesDown(t *testing.T) {
	r, m, now := tickerFixture(t)
	tickerIs(t, m)
	a := issue(t, r, "thing.build")
	b := issue(t, r, "thing.ship")
	tickerIs(t, m, "thing.ship running", "thing.build running")
	// The newer job finishes: it falls below the running one.
	r.Settle(b, nil)
	tickerIs(t, m, "thing.build running", "thing.ship done")
	// A third starts: it takes the top, the finished one is pushed off.
	c := issue(t, r, "thing.build")
	tickerIs(t, m, "thing.build running", "thing.build running")
	// A failure carries its error and stays below the running job.
	r.Settle(a, errors.New("boom"))
	tickerIs(t, m, "thing.build running", "thing.build failed: boom")
	// jobs.list deletes finished jobs on read; the ticker remembers them.
	if _, err := r.Execute(Request{Verb: "list", Noun: "jobs"}); err != nil {
		t.Fatal(err)
	}
	tickerIs(t, m, "thing.build running", "thing.build failed: boom")
	// Past JobLinger the finished row goes at the next frame.
	*now = now.Add(JobLinger)
	tickerIs(t, m, "thing.build running")
	r.Settle(c, nil)
	tickerIs(t, m, "thing.build done")
	*now = now.Add(JobLinger)
	tickerIs(t, m)
}

// Quiet verbs and framework jobs (an unregistered verb such as a
// terminal's read) never show.
func TestJobsTickerHidesQuietAndFrameworkJobs(t *testing.T) {
	r, m, _ := tickerFixture(t)
	r.Settle(issue(t, r, "thing.reload"), nil)
	issue(t, r, "thing.reload")
	issue(t, r, "read")
	tickerIs(t, m)
	if !r.Operations["thing.reload"].Quiet || r.Operations["thing.build"].Quiet {
		t.Fatal("Quiet did not mark only thing.reload")
	}
}

func TestJobsTickerTruncatesAndTakesRowsFromTheBody(t *testing.T) {
	r, m, _ := tickerFixture(t)
	r.Settle(issue(t, r, "thing.build"), errors.New("a very long failure message that will not fit"))
	m.Update(frameMsg)
	if got := m.JobsTicker(20); len(got) != 1 || got[0] != "thing.build failed:…" {
		t.Fatalf("truncated = %q", got)
	}
	_, full := m.layout().BodySize()
	issue(t, r, "thing.ship")
	m.Update(frameMsg)
	if _, h := m.pageSize(); h != full-2 {
		t.Fatalf("page height %d with two ticker rows, want %d", h, full-2)
	}
	rows := strings.Split(ansi.Strip(m.View().Content), "\n")
	if got := []string{strings.TrimSpace(rows[19]), strings.TrimSpace(rows[20])}; got[0] != "thing.ship running" || !strings.HasPrefix(got[1], "thing.build failed: a very long") || !strings.HasPrefix(strings.TrimSpace(rows[21]), "─") {
		t.Fatalf("ticker rows %q, rule %q", got, rows[21])
	}
}
