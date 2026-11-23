// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"codeberg.org/tslocum/cbind"
	"github.com/PalWorks/whatscli/config"
	"github.com/PalWorks/whatscli/messages"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

// Tests never keep the Recent list in the real state directory: unless a
// test names a temporary file, nothing is kept.
func init() { recentEmojiPath = func() string { return "" } }

// useRecentFile points the Recent list at a file in a temporary directory
// and forgets the list loaded so far.
func useRecentFile(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "recent_emoji")
	recentEmojiPath = func() string { return path }
	recentLoaded, recentFile, recentList = false, "", nil
	t.Cleanup(func() {
		recentSaveWait.Wait()
		recentEmojiPath = func() string { return "" }
		recentLoaded, recentFile, recentList = false, "", nil
	})
	return path
}

// runPickerApp runs a tview app on a simulated w x h screen laid out like
// the real one (pages over a message view and the input line), with the
// global keys, the emoji_picker key (Ctrl+t) and, if compose is set, the
// compose features attached.
func runPickerApp(t *testing.T, w, h int, compose bool) (*tview.Application, *tview.InputField, tcell.SimulationScreen, chan messages.Command) {
	t.Helper()
	useRecentFile(t)
	screen := tcell.NewSimulationScreen("UTF-8")
	if err := screen.Init(); err != nil {
		t.Fatal(err)
	}
	app := tview.NewApplication().SetScreen(screen).EnableMouse(true)
	screen.SetSize(w, h) // after SetScreen, which initialises the screen again
	input := tview.NewInputField()
	view := tview.NewTextView()
	input.SetDoneFunc(EnterCommand)
	setupCommandCompletion(input)
	layout := tview.NewFlex().SetDirection(tview.FlexRow).AddItem(view, 0, 1, false).AddItem(input, 1, 0, true)
	cmds := make(chan messages.Command, 8)
	savedCtx, savedPages, savedShare := ctx, pages, config.Config.General.SharePresence
	config.Config.General.SharePresence = false // no network in tests
	ctx = &AppContext{App: app, TextInput: input, TextView: view, KeyBindings: cbind.NewConfiguration(),
		SessionManager: &messages.SessionManager{CommandChannel: cmds}}
	if err := setEmojiPickerKey(ctx.KeyBindings, "Ctrl+t"); err != nil {
		t.Fatal(err)
	}
	pages = tview.NewPages().AddPage("main", layout, true, true)
	app.SetInputCapture(globalKeys)
	if compose {
		view.SetDynamicColors(true).SetRegions(true)
		setupCompose()
	}
	done := make(chan struct{})
	go func() { _ = app.SetRoot(pages, true).Run(); close(done) }()
	t.Cleanup(func() {
		app.Stop()
		<-done
		ctx, pages = savedCtx, savedPages
		config.Config.General.SharePresence = savedShare
		overlayName, overlayView, overlayFocus, activeEmojiPicker = "", nil, nil, nil
		drafts = map[string]string{}
		editing, replying = nil, nil
	})
	return app, input, screen, cmds
}

// key delivers a key press the way the app does (its input capture, then
// the focused page) on the event loop, and waits until it is handled and
// drawn.
func key(app *tview.Application, screen tcell.SimulationScreen, k tcell.Key, r rune, mod tcell.ModMask) {
	run(app, func() {
		if ev := globalKeys(tcell.NewEventKey(k, r, mod)); ev != nil {
			pages.InputHandler()(ev, func(p tview.Primitive) { app.SetFocus(p) })
		}
	})
	settle(app)
}

// click injects a left click at x, y into the simulated terminal and waits
// until cond holds.
func click(t *testing.T, app *tview.Application, screen tcell.SimulationScreen, x, y int, cond func() bool) {
	t.Helper()
	screen.InjectMouse(x, y, tcell.Button1, tcell.ModNone)
	screen.InjectMouse(x, y, tcell.ButtonNone, tcell.ModNone)
	if !waitFor(app, cond) {
		t.Fatalf("click at %d,%d: condition not met", x, y)
	}
}

func ctrlT(app *tview.Application, screen tcell.SimulationScreen) {
	key(app, screen, tcell.KeyCtrlT, 0, tcell.ModCtrl)
}

func pickerOpen(app *tview.Application) bool {
	var open bool
	run(app, func() { open = overlayName == emojiOverlay && activeEmojiPicker != nil })
	return open
}

// pickerState returns the open picker's state, read on the event loop.
func pickerState(app *tview.Application) (cat, sel int, query string, items []int) {
	run(app, func() {
		if p := activeEmojiPicker; p != nil {
			cat, sel, query, items = p.cat, p.sel, p.query, slices.Clone(p.items)
		}
	})
	return
}

func TestEmojiPickerData(t *testing.T) {
	pickerOnce.Do(loadPickerEmoji)
	if len(pickerAll) < 1000 {
		t.Fatalf("only %d emoji loaded", len(pickerAll))
	}
	for cat := 1; cat < len(emojiCategories); cat++ {
		if len(pickerByCat[cat]) == 0 {
			t.Errorf("category %q is empty", emojiCategories[cat].name)
		}
	}
	seen := map[string]bool{}
	for _, p := range pickerAll {
		if dropSkinTones(p.e) != p.e {
			t.Errorf("skin tone variant %q (%s)", p.e, p.name)
		}
		if strings.ContainsAny(p.e, "‍⃣") {
			t.Errorf("ZWJ or keycap sequence %q (%s)", p.e, p.name)
		}
		for _, r := range p.e {
			if r >= 0xE0020 && r <= 0xE007F {
				t.Errorf("tag sequence %q (%s)", p.e, p.name)
			}
			if r >= 0x1F1E6 && r <= 0x1F1FF {
				t.Errorf("flag %q (%s): the Flags group is left out", p.e, p.name)
			}
		}
		if w := tview.TaggedStringWidth(p.e); w != 2 {
			t.Errorf("%q (%s) is %d columns wide, want 2", p.e, p.name, w)
		}
		if seen[emojiKey(p.e)] {
			t.Errorf("duplicate %q (%s)", p.e, p.name)
		}
		seen[emojiKey(p.e)] = true
		if p.name == "" || p.lower != strings.ToLower(p.name) {
			t.Errorf("bad name %q for %q", p.name, p.e)
		}
	}
	for _, want := range []string{"😀", "👍", "❤️", "🐶", "🍕", "🚗", "⚽", "💡", "✅"} {
		if !seen[emojiKey(want)] {
			t.Errorf("%q missing", want)
		}
	}
	if emojiDataVersion == "" {
		t.Error("no Emoji version recorded")
	}
	t.Logf("%d emoji in the picker, Emoji %s", len(pickerAll), emojiDataVersion)
}

func TestSearchEmojiRanking(t *testing.T) {
	got := searchEmoji("CAT")
	if len(got) == 0 {
		t.Fatal("no match for cat")
	}
	prefixDone := false
	cats := map[int]bool{}
	for _, i := range got {
		name := pickerAll[i].lower
		if !strings.Contains(name, "cat") {
			t.Errorf("%q does not match", name)
		}
		if !strings.HasPrefix(name, "cat") {
			prefixDone = true
		} else if prefixDone {
			t.Errorf("prefix match %q after a substring match", name)
		}
		cats[pickerAll[i].cat] = true
	}
	if !strings.HasPrefix(pickerAll[got[0]].lower, "cat") {
		t.Errorf("first result %q should start with cat", pickerAll[got[0]].name)
	}
	if len(cats) < 2 {
		t.Errorf("search should cover every category, got %v", cats)
	}
	if n := len(searchEmoji("thumbs up")); n != 1 {
		t.Errorf("thumbs up: %d results, want 1", n)
	}
	if len(searchEmoji("zzzznotanemoji")) != 0 {
		t.Error("nonsense matched")
	}
	if got := searchEmoji("flag"); len(got) != 0 {
		for _, i := range got {
			if pickerAll[i].cat == 0 || strings.HasPrefix(pickerAll[i].lower, "flag:") {
				t.Errorf("search finds a country flag: %q", pickerAll[i].name)
			}
		}
	}
}

func TestAddRecent(t *testing.T) {
	list := addRecent(nil, "😀")
	list = addRecent(list, "👍")
	list = addRecent(list, "😀")
	if !slices.Equal(list, []string{"😀", "👍"}) {
		t.Fatalf("list = %q", list)
	}
	// The same emoji without its U+FE0F is the same entry.
	list = addRecent([]string{"❤️", "👍"}, "❤")
	if !slices.Equal(list, []string{"❤", "👍"}) {
		t.Fatalf("FE0F variant not merged: %q", list)
	}
	var many []string
	for _, i := range searchEmoji("face")[:30] {
		many = addRecent(many, pickerAll[i].e)
	}
	if len(many) != emojiRecentMax {
		t.Fatalf("len = %d, want %d", len(many), emojiRecentMax)
	}
	before := slices.Clone(many)
	_ = addRecent(many, "🍕")
	if !slices.Equal(many, before) {
		t.Error("addRecent changed its argument")
	}
}

func TestRecentEmojiFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "recent_emoji")
	if got := readRecentEmoji(path); got != nil {
		t.Fatalf("missing file: %q", got)
	}
	if got := readRecentEmoji(""); got != nil {
		t.Fatalf("no path: %q", got)
	}
	want := []string{"🍕", "❤️", "✅"}
	if err := writeRecentEmoji(path, want); err != nil {
		t.Fatal(err)
	}
	if got := readRecentEmoji(path); !slices.Equal(got, want) {
		t.Fatalf("round trip = %q, want %q", got, want)
	}
	// Junk, unknown lines, skin tones, duplicates and a variant without
	// U+FE0F are skipped or merged; the file is not trusted.
	junk := "\x00\xff\xfe garbage\nnot an emoji\n👍🏽\n🇮🇳\n🍕\n🍕\n❤\n\n[red]x\n"
	if err := os.WriteFile(path, []byte(junk), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readRecentEmoji(path); !slices.Equal(got, []string{"🍕", "❤️"}) {
		t.Fatalf("corrupt file read as %q", got)
	}
	// A directory in place of the file, or an unwritable place, is ignored.
	if got := readRecentEmoji(dir); got != nil {
		t.Fatalf("directory read as %q", got)
	}
	if err := writeRecentEmoji(filepath.Join(dir, "missing", "recent_emoji"), want); err == nil {
		t.Fatal("write into a missing directory should fail (and is ignored by the picker)")
	}
}

// testPicker is a picker laid out in a w x h area, not drawn.
func testPicker(t *testing.T, w, h int) *emojiPicker {
	t.Helper()
	useRecentFile(t)
	loadRecent()
	p := newEmojiPicker()
	p.SetRect(0, 0, w, h)
	return p
}

func TestEmojiPickerNavigation(t *testing.T) {
	p := testPicker(t, 40, 10) // 12 columns, 4 rows
	l := p.layout()
	if l.cols != 12 || l.rows != 4 {
		t.Fatalf("layout %d x %d, want 12 x 4", l.cols, l.rows)
	}
	if p.cat != 1 {
		t.Fatalf("with no recent emoji the picker opens on Smileys, got %d", p.cat)
	}
	n := len(p.items)
	steps := []struct {
		key  tcell.Key
		want int
	}{
		{tcell.KeyLeft, 0},  // clamps at the first
		{tcell.KeyUp, 0},    // first row: stays
		{tcell.KeyRight, 1}, //
		{tcell.KeyDown, 13}, //
		{tcell.KeyUp, 1},    //
		{tcell.KeyHome, 0},  //
		{tcell.KeyPgDn, 48}, // 12 x 4
		{tcell.KeyPgUp, 0},  //
		{tcell.KeyEnd, n - 1},
		{tcell.KeyRight, n - 1}, // clamps at the last
		{tcell.KeyDown, n - 1},  // last row: stays
	}
	for i, s := range steps {
		p.move(s.key)
		if p.sel != s.want {
			t.Fatalf("step %d (%v): sel = %d, want %d", i, tcell.KeyNames[s.key], p.sel, s.want)
		}
	}
	// Right at the end of a row goes to the start of the next, Left back.
	p.sel = 11
	p.move(tcell.KeyRight)
	if p.sel != 12 {
		t.Fatalf("Right from the row end: %d, want 12", p.sel)
	}
	p.move(tcell.KeyLeft)
	if p.sel != 11 {
		t.Fatalf("Left from the row start: %d, want 11", p.sel)
	}
	// PgUp from inside the first page goes to the first row, same column.
	p.sel = 12*2 + 5
	p.move(tcell.KeyPgUp)
	if p.sel != 5 {
		t.Fatalf("PgUp: %d, want 5", p.sel)
	}
	// PgDn near the end goes to the last row, same column (or the last).
	p.sel = n - 20
	p.move(tcell.KeyPgDn)
	if last := (n - 1) / 12 * 12; p.sel < last || p.sel > n-1 {
		t.Fatalf("PgDn near the end: %d, want in the last row [%d, %d]", p.sel, last, n-1)
	}
	// The selected row stays in view.
	if row := p.sel / l.cols; row < p.top || row >= p.top+l.rows {
		t.Fatalf("selection row %d not in view (top %d)", row, p.top)
	}
	// Down onto a short last row clamps to its last emoji.
	p.sel = (n-1)/12*12 - 1 // end of the row before the last
	if (n-1)%12 != 11 {
		p.move(tcell.KeyDown)
		if p.sel != n-1 {
			t.Fatalf("Down onto a short row: %d, want %d", p.sel, n-1)
		}
	}
}

func TestEmojiPickerCategories(t *testing.T) {
	p := testPicker(t, 80, 22)
	in := p.InputHandler()
	press := func(k tcell.Key, r rune) { in(tcell.NewEventKey(k, r, tcell.ModNone), func(tview.Primitive) {}) }
	press(tcell.KeyRight, 0)
	press(tcell.KeyTab, 0)
	if p.cat != 2 || p.sel != 0 || p.items[0] != pickerByCat[2][0] {
		t.Fatalf("Tab: cat %d sel %d", p.cat, p.sel)
	}
	press(tcell.KeyBacktab, 0)
	press(tcell.KeyBacktab, 0)
	if p.cat != 0 || len(p.items) != 0 {
		t.Fatalf("Shift+Tab to Recent: cat %d, %d items", p.cat, len(p.items))
	}
	press(tcell.KeyBacktab, 0)
	if len(emojiCategories) != 9 || p.cat != 8 {
		t.Fatalf("Shift+Tab wraps to Symbols (8 of 9 tabs, no Flags), got %d of %d", p.cat, len(emojiCategories))
	}
	press(tcell.KeyTab, 0)
	if p.cat != 0 {
		t.Fatalf("Tab wraps to Recent, got %d", p.cat)
	}
	// While searching, Tab does nothing and every category is searched.
	for _, r := range "Pizza" {
		press(tcell.KeyRune, r)
	}
	press(tcell.KeyTab, 0)
	if p.query != "Pizza" || p.cat != 0 || len(p.items) != 1 || pickerAll[p.items[0]].e != "🍕" {
		t.Fatalf("search: query %q cat %d items %d", p.query, p.cat, len(p.items))
	}
	press(tcell.KeyBackspace2, 0)
	if p.query != "Pizz" {
		t.Fatalf("Backspace: %q", p.query)
	}
}

func TestTabLabelsFit(t *testing.T) {
	for _, w := range []int{24, 30, 38, 50, 60, 78} {
		for cur := range emojiCategories {
			labels, first, last := tabLabels(w, cur)
			if cur < first || cur > last {
				t.Errorf("width %d: current tab %d not shown (%d..%d)", w, cur, first, last)
			}
			width := last - first
			for i := first; i <= last; i++ {
				width += len(labels[i])
			}
			if first > 0 || last < len(labels)-1 {
				width += 2
			}
			if width > w {
				t.Errorf("width %d, tab %d: labels need %d columns", w, cur, width)
			}
		}
	}
	if labels, first, last := tabLabels(78, 0); first != 0 || last != len(labels)-1 || labels[1] != "Smileys" {
		t.Errorf("78 columns should show every short label: %q %d..%d", labels, first, last)
	}
}

// gridRow checks one drawn grid row: an emoji every 3 columns from the
// grid's left edge, nothing else, and the border intact.
func checkGrid(t *testing.T, screen tcell.SimulationScreen, l pickerLayout, want int) {
	t.Helper()
	cells, w, _ := screen.GetContents()
	at := func(x, y int) []rune { return cells[y*w+x].Runes }
	shown := 0
	for r := 0; r < l.rows; r++ {
		y := l.gridY + r
		for x := l.ix; x < l.ix+l.iw; x++ {
			rs := at(x, y)
			isEmoji := len(rs) > 0 && rs[0] > 0x7F
			onCell := x >= l.gridX && (x-l.gridX)%emojiCellWidth == 0 && (x-l.gridX)/emojiCellWidth < l.cols
			if isEmoji && !onCell {
				t.Fatalf("row %d: emoji %q at column %d, off the 3-column grid", r, string(rs), x)
			}
			if isEmoji {
				shown++
				if next := at(x+2, y); len(next) > 0 && next[0] != ' ' {
					t.Fatalf("row %d: no space after the emoji at %d: %q", r, x, string(next))
				}
			}
		}
		for _, x := range []int{l.bx, l.bx + l.bw - 1} {
			if rs := at(x, y); len(rs) == 0 || rs[0] != tview.Borders.Vertical && rs[0] != tview.Borders.VerticalFocus {
				t.Fatalf("row %d: border at %d overwritten by %q", r, x, string(rs))
			}
		}
	}
	if shown != want {
		t.Fatalf("%d emoji drawn, want %d", shown, want)
	}
}

func TestEmojiPickerRendersAligned(t *testing.T) {
	for _, w := range []int{40, 80, 120} {
		t.Run(fmt.Sprint(w), func(t *testing.T) {
			app, _, screen, _ := runPickerApp(t, w, 30, false)
			ctrlT(app, screen)
			if !pickerOpen(app) {
				t.Fatal("Ctrl+t did not open the picker")
			}
			var l pickerLayout
			var n int
			run(app, func() { l = activeEmojiPicker.layout(); n = len(activeEmojiPicker.items) })
			if l.cols != (min(w, emojiPickerMaxWidth)-2)/3 {
				t.Fatalf("width %d: %d columns", w, l.cols)
			}
			settle(app)
			app.QueueUpdateDraw(func() {})
			settle(app)
			checkGrid(t, screen, l, min(n, l.cols*l.rows))
			text := screenText(screen)
			for _, s := range []string{"Search:", "grinning face", "Esc close"} {
				if !strings.Contains(text, s) {
					t.Fatalf("width %d: %q not shown:\n%s", w, s, text)
				}
			}
			if w >= 80 && !strings.Contains(text, "Smileys") {
				t.Fatalf("width %d: tab labels not shown:\n%s", w, text)
			}
			if w == 40 && !strings.Contains(text, "Smi") {
				t.Fatalf("width 40: short tab labels not shown:\n%s", text)
			}
		})
	}
}

// Enter adds the selected emoji and the picker stays open; three picks in a
// row land in order at the cursor, and typing goes on after the last.
func TestEmojiPickerPicksSeveral(t *testing.T) {
	app, input, screen, cmds := runPickerApp(t, 100, 30, true)
	run(app, func() { enterChat(chatA) })
	typeKeys(app, input, "hello world", tcell.KeyHome)
	ctrlT(app, screen)
	_, _, _, items := pickerState(app)
	want := ""
	for i := 0; i < 3; i++ {
		want += pickerAll[items[i]].e
		key(app, screen, tcell.KeyEnter, 0, tcell.ModNone)
		key(app, screen, tcell.KeyRight, 0, tcell.ModNone)
		if !pickerOpen(app) {
			t.Fatalf("picker closed after pick %d", i+1)
		}
	}
	if got := inputText(app, input); got != want+"hello world" {
		t.Fatalf("input = %q, want %q", got, want+"hello world")
	}
	key(app, screen, tcell.KeyEscape, 0, tcell.ModNone)
	if pickerOpen(app) {
		t.Fatal("Esc with no search did not close")
	}
	var focused bool
	run(app, func() { focused = app.GetFocus() == input })
	if !focused {
		t.Fatal("focus did not return to the input")
	}
	key(app, screen, tcell.KeyRune, 'x', tcell.ModNone)
	if got := inputText(app, input); got != want+"xhello world" {
		t.Fatalf("cursor not after the last emoji: %q", got)
	}
	if drafts[chatA.Id] != want+"xhello world" {
		t.Fatalf("draft = %q", drafts[chatA.Id])
	}
	select {
	case c := <-cmds:
		t.Fatalf("picking sent %+v", c)
	default:
	}
	recentSaveWait.Wait()
	var rec []string
	run(app, func() { rec = slices.Clone(recentList) })
	if len(rec) != 3 || rec[0] != pickerAll[items[2]].e || rec[2] != pickerAll[items[0]].e {
		t.Fatalf("recent = %q", rec)
	}
	// The Recent file holds the same list, and the next open shows it.
	if got := readRecentEmoji(recentFile); !slices.Equal(got, rec) {
		t.Fatalf("recent file = %q, want %q", got, rec)
	}
	ctrlT(app, screen)
	if cat, _, _, items := pickerState(app); cat != 0 || len(items) != 3 {
		t.Fatalf("reopened on cat %d with %d items, want Recent with 3", cat, len(items))
	}
}

// The emoji_picker key closes the picker again; Esc first clears a search.
func TestEmojiPickerToggleAndEsc(t *testing.T) {
	app, input, screen, _ := runPickerApp(t, 100, 30, false)
	ctrlT(app, screen)
	if !pickerOpen(app) {
		t.Fatal("not open")
	}
	ctrlT(app, screen)
	if pickerOpen(app) {
		t.Fatal("Ctrl+t did not close the picker")
	}
	ctrlT(app, screen)
	for _, r := range "dog" {
		key(app, screen, tcell.KeyRune, r, tcell.ModNone)
	}
	if _, _, q, items := pickerState(app); q != "dog" || len(items) == 0 {
		t.Fatalf("query %q, %d items", q, len(items))
	}
	if !screenContains(screen, "Search: dog") {
		t.Fatalf("search line not shown:\n%s", screenText(screen))
	}
	key(app, screen, tcell.KeyEscape, 0, tcell.ModNone)
	if _, _, q, _ := pickerState(app); q != "" || !pickerOpen(app) {
		t.Fatalf("Esc with a search: query %q, open %v", q, pickerOpen(app))
	}
	key(app, screen, tcell.KeyEscape, 0, tcell.ModNone)
	if pickerOpen(app) {
		t.Fatal("second Esc did not close")
	}
	if got := inputText(app, input); got != "" {
		t.Fatalf("input changed: %q", got)
	}
}

// A click on an emoji adds it (the picker stays open), a click on a tab
// switches to it, the wheel scrolls, a click outside closes.
func TestEmojiPickerMouse(t *testing.T) {
	app, input, screen, _ := runPickerApp(t, 120, 10, false) // 4 grid rows, so the wheel can scroll
	ctrlT(app, screen)
	var l pickerLayout
	var items []int
	var tabs []pickerTab
	run(app, func() {
		l, items, tabs = activeEmojiPicker.layout(), slices.Clone(activeEmojiPicker.items), slices.Clone(activeEmojiPicker.tabs)
	})
	want := pickerAll[items[l.cols+2]].e
	click(t, app, screen, l.gridX+emojiCellWidth*2, l.gridY+1, func() bool { return input.GetText() == want }) // row 1, column 2
	want += pickerAll[items[0]].e
	// The second half of the first emoji; a quick second click counts too.
	click(t, app, screen, l.gridX+1, l.gridY, func() bool { return input.GetText() == want })
	if !pickerOpen(app) {
		t.Fatal("picker closed after a click on an emoji")
	}
	var food pickerTab
	for _, tb := range tabs {
		if tb.cat == 4 {
			food = tb
		}
	}
	click(t, app, screen, food.x0, l.iy, func() bool { return activeEmojiPicker.cat == 4 })
	screen.InjectMouse(l.gridX, l.gridY, tcell.WheelDown, tcell.ModNone)
	if !waitFor(app, func() bool { return activeEmojiPicker.top == 1 }) {
		t.Fatal("wheel did not scroll")
	}
	click(t, app, screen, 1, 1, func() bool { return overlayName == "" }) // outside the window
	if got := inputText(app, input); got != want {
		t.Fatalf("input changed by the closing click: %q", got)
	}
}

// Picking on the Recent tab updates it at once; the selection keeps its
// place.
func TestEmojiPickerRecentLive(t *testing.T) {
	app, input, screen, _ := runPickerApp(t, 100, 30, false)
	run(app, func() { recentLoaded, recentList = true, []string{"🍕", "🐶", "🚗"} })
	ctrlT(app, screen)
	key(app, screen, tcell.KeyRight, 0, tcell.ModNone)
	key(app, screen, tcell.KeyEnter, 0, tcell.ModNone)
	cat, sel, _, items := pickerState(app)
	if cat != 0 || sel != 1 {
		t.Fatalf("cat %d sel %d", cat, sel)
	}
	var got []string
	for _, i := range items {
		got = append(got, pickerAll[i].e)
	}
	if !slices.Equal(got, []string{"🐶", "🍕", "🚗"}) || inputText(app, input) != "🐶" {
		t.Fatalf("recent shown %q, input %q", got, inputText(app, input))
	}
}

// Picking in edit and reply modes adds to the text and keeps the mode; it
// never sends.
func TestEmojiPickerKeepsEditAndReply(t *testing.T) {
	app, input, screen, cmds := runPickerApp(t, 100, 30, true)
	now := uint64(time.Now().Unix())
	run(app, func() {
		enterChat(chatA)
		ctx.CurRegions = []messages.Message{{Id: "m1", ChatId: chatA.Id, FromMe: true, Text: "hi", Timestamp: now - 30}}
	})
	typeKeys(app, input, "draft")
	run(app, func() { setInputText("") })
	press(app, input, tcell.NewEventKey(tcell.KeyUp, 0, tcell.ModNone))
	ctrlT(app, screen)
	key(app, screen, tcell.KeyEnter, 0, tcell.ModNone)
	ctrlT(app, screen)
	_, _, _, items := pickerState(app)
	_ = items
	first := pickerAll[pickerByCat[1][0]].e
	var label string
	run(app, func() { label = input.GetLabel() })
	if got := inputText(app, input); got != "hi"+first || editing == nil || !strings.Contains(label, "edit>") {
		t.Fatalf("edit: input %q, editing %v, label %q", got, editing != nil, label)
	}
	if drafts[chatA.Id] != "draft" {
		t.Fatalf("edit changed the draft: %q", drafts[chatA.Id])
	}
	key(app, screen, tcell.KeyEscape, 0, tcell.ModNone) // leaves edit mode
	run(app, func() { setInputText(""); startReply(ctx.CurRegions[0]) })
	ctrlT(app, screen) // opens on Recent now: the first emoji again
	key(app, screen, tcell.KeyEnter, 0, tcell.ModNone)
	key(app, screen, tcell.KeyEscape, 0, tcell.ModNone)
	run(app, func() { label = input.GetLabel() })
	if got := inputText(app, input); got != first || replying == nil || !strings.Contains(label, "↳") {
		t.Fatalf("reply: input %q, replying %v, label %q", got, replying != nil, label)
	}
	select {
	case c := <-cmds:
		t.Fatalf("picking sent %+v", c)
	default:
	}
}

// /emoji opens the picker and brings back the draft the command line hid.
func TestEmojiCommandOpensPickerWithDraft(t *testing.T) {
	app, input, _, _ := runPickerApp(t, 100, 30, true)
	run(app, func() { enterChat(chatA) })
	typeKeys(app, input, "my draft")
	run(app, func() { setInputText("") })
	run(app, func() { openEmojiPicker(true) })
	if !pickerOpen(app) || inputText(app, input) != "my draft" {
		t.Fatalf("open %v, input %q", pickerOpen(app), inputText(app, input))
	}
}
