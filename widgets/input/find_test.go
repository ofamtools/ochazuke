package input

import "testing"

func TestFindCharMotionsAndOperators(t *testing.T) {
	m := New("alpha, beta, gamma", false)
	// f, jumps to first comma (index 5), ; repeats to second comma (index 11), , reverses back to 5.
	m.HandleKey("f")
	m.HandleKey(",")
	if m.Cursor() != 5 {
		t.Fatalf("f, cursor = %d, want 5", m.Cursor())
	}
	m.HandleKey(";")
	if m.Cursor() != 11 {
		t.Fatalf("; cursor = %d, want 11", m.Cursor())
	}
	m.HandleKey(",")
	if m.Cursor() != 5 {
		t.Fatalf(", cursor = %d, want 5", m.Cursor())
	}

	// t g stops one char before 'g' (at index 12, space before gamma).
	m.HandleKey("t")
	m.HandleKey("g")
	if m.Cursor() != 12 {
		t.Fatalf("tg cursor = %d, want 12", m.Cursor())
	}

	// F a searches backward to 'a' in beta (index 10).
	m.HandleKey("F")
	m.HandleKey("a")
	if m.Cursor() != 10 {
		t.Fatalf("Fa cursor = %d, want 10", m.Cursor())
	}

	// Operator composition: df, deletes through the comma inclusively.
	m2 := New("one, two, three", false)
	m2.HandleKey("d")
	m2.HandleKey("f")
	m2.HandleKey(",")
	if m2.Value() != " two, three" {
		t.Fatalf("df, value = %q, want %q", m2.Value(), " two, three")
	}

	// Operator composition: ct, changes up to (excluding) the comma.
	m3 := New("hello, world", false)
	m3.HandleKey("c")
	m3.HandleKey("t")
	m3.HandleKey(",")
	m3.HandleInput("x", "hi")
	m3.HandleKey("esc")
	if m3.Value() != "hi, world" {
		t.Fatalf("ct, value = %q, want %q", m3.Value(), "hi, world")
	}
}

func TestContextUnitMotionsParens(t *testing.T) {
	// Default sentence motion.
	m := New("First sentence. Second one! Third.", false)
	m.HandleKey(")")
	if m.Cursor() != 16 {
		t.Fatalf("default ) cursor = %d, want 16 (%q)", m.Cursor(), m.Value()[m.Cursor():])
	}
	m.HandleKey("(")
	if m.Cursor() != 0 {
		t.Fatalf("default ( cursor = %d, want 0", m.Cursor())
	}

	// CSV scope: ) jumps to next CSV cell.
	c := New("name,role,team", false)
	c.SetWordScope("csv")
	c.HandleKey(")")
	if c.Cursor() != 5 {
		t.Fatalf("csv ) cursor = %d, want 5 (%q)", c.Cursor(), c.Value()[c.Cursor():])
	}
}
