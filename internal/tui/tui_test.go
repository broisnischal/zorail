package tui

import "testing"

func TestPaneAt(t *testing.T) {
	m := Model{}
	// Three-pane layout: inboxes [0,30) · messages [30,90) · reader [90,…).
	d := paneDims{leftW: 30, msgW: 60, readerW: 40}
	cases := []struct {
		x    int
		want focus
	}{
		{5, focusInboxes}, {29, focusInboxes},
		{30, focusMessages}, {89, focusMessages},
		{90, focusReader}, {200, focusReader},
	}
	for _, c := range cases {
		if got := m.paneAt(c.x, d); got != c.want {
			t.Errorf("paneAt(%d) = %v, want %v", c.x, got, c.want)
		}
	}
	// Two-pane (no reader): everything right of inboxes is messages.
	d2 := paneDims{leftW: 30, msgW: 70, readerW: 0}
	if got := m.paneAt(120, d2); got != focusMessages {
		t.Errorf("two-pane paneAt(120) = %v, want messages", got)
	}
}

func TestRowIndexAt(t *testing.T) {
	const innerH = 20 // vis = (20-2)/2 = 9 rows visible
	// cursor at top → window starts at 0.
	cases := []struct {
		y, cursor, want int
	}{
		{1, 0, -1},  // box top border
		{2, 0, -1},  // title
		{3, 0, -1},  // blank
		{4, 0, 0},   // first row, line 1
		{5, 0, 0},   // first row, line 2
		{6, 0, 1},   // second row
		{22, 0, -1}, // below content (innerH area is rows 2..21)
	}
	for _, c := range cases {
		if got := rowIndexAt(c.y, innerH, c.cursor); got != c.want {
			t.Errorf("rowIndexAt(y=%d, cursor=%d) = %d, want %d", c.y, c.cursor, got, c.want)
		}
	}
	// Scrolled: cursor past the fold shifts the visible window's first item.
	// vis=9, cursor=15 → start = 15-9+1 = 7, so the top visible row is item 7.
	if got := rowIndexAt(4, innerH, 15); got != 7 {
		t.Errorf("scrolled rowIndexAt = %d, want 7", got)
	}
}

func TestCurrentLinks(t *testing.T) {
	m := Model{current: &FullMsg{
		Extracted: Extracted{Links: []string{
			"https://a.test/x?u=1&amp;v=2", // &amp; must be repaired
			"https://dup.test/",
			"https://dup.test/", // exact dupe dropped
			"https://b.test/y",
		}},
	}}
	got := m.currentLinks()
	want := []string{"https://a.test/x?u=1&v=2", "https://dup.test/", "https://b.test/y"}
	if len(got) != len(want) {
		t.Fatalf("currentLinks len = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("link %d = %q, want %q", i, got[i], want[i])
		}
	}
	// No message → no links, no panic.
	if l := (Model{}).currentLinks(); l != nil {
		t.Errorf("nil-message currentLinks = %v, want nil", l)
	}
}
