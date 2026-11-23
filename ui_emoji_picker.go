// SPDX-License-Identifier: AGPL-3.0-only

package main

// ui_emoji_picker.go: a WhatsApp-style emoji picker over the main screen.
//
// The emoji_picker key (default Ctrl+t) or /emoji opens it. It shows a row of
// category tabs (Recent first), a search line and a grid of emoji. Arrow keys
// move, Tab and Shift+Tab switch category, PgUp and PgDn move a page; typed
// letters search every category by CLDR name. Enter or a click adds the
// selected emoji to the input line at the cursor and the picker stays open,
// so several can be added; Esc clears the search, then closes; the key again
// or a click outside closes it too.
//
// The emoji come from emoji_data.go (generated from Unicode's
// emoji-test.txt by tools/gen-emoji-data): fully-qualified emoji only, no
// skin tones, no ZWJ or keycap sequences. Every grid cell is 3 columns wide
// (a 2-column emoji plus a space): 2-column emoji are the norm in terminals,
// and any emoji whose measured width is not 2 is left out so the grid stays
// aligned. The data is parsed on first open (sync.Once), not at start.
//
// Recently picked emoji (at most 24, newest first) are kept in a small text
// file, one emoji per line (config.GetRecentEmojiFilePath); errors reading
// or writing it are ignored. Everything here runs on the UI goroutine except
// the file write, which runs in a goroutine.

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"codeberg.org/tslocum/cbind"
	"github.com/PalWorks/whatscli/config"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

const (
	// emojiOverlay is the page name of the picker (overlayName while open).
	emojiOverlay = "emoji"
	// emojiCellWidth is the width of one grid cell: a 2-column emoji and a
	// space.
	emojiCellWidth = 3
	// emojiRecentMax caps the Recent tab.
	emojiRecentMax = 24
	// emojiPickerMaxWidth and emojiPickerMaxHeight cap the window; on a
	// smaller terminal it takes the whole screen. About 40 columns is the
	// least that is usable.
	emojiPickerMaxWidth  = 80
	emojiPickerMaxHeight = 22
)

// emojiCategory is one tab: its full name (the group in emoji-test.txt),
// a short label and a three-letter label for narrow terminals.
type emojiCategory struct{ name, short, tiny string }

// emojiCategories are the tabs in order; index 0 is Recent, the others are
// the groups of emoji_data.go.
var emojiCategories = []emojiCategory{
	{"Recent", "Recent", "Rec"},
	{"Smileys & Emotion", "Smileys", "Smi"},
	{"People & Body", "People", "Ppl"},
	{"Animals & Nature", "Animals", "Ani"},
	{"Food & Drink", "Food", "Fod"},
	{"Travel & Places", "Travel", "Trv"},
	{"Activities", "Activities", "Act"},
	{"Objects", "Objects", "Obj"},
	{"Symbols", "Symbols", "Sym"},
	{"Flags", "Flags", "Flg"},
}

// pickerEmoji is one emoji of the picker.
type pickerEmoji struct {
	e     string // the emoji
	name  string // CLDR name
	lower string // name in lower case, for search
	cat   int    // index in emojiCategories (1 or more)
}

var (
	pickerOnce  sync.Once
	pickerAll   []pickerEmoji  // every emoji, in category then CLDR order
	pickerByCat [][]int        // category -> indices into pickerAll
	pickerIndex map[string]int // emoji without U+FE0F -> index into pickerAll
)

// emojiKey is the emoji without the U+FE0F presentation selector, so an
// emoji stored with and without it is the same.
func emojiKey(e string) string { return strings.ReplaceAll(e, "️", "") }

// loadPickerEmoji parses emojiData. Emoji whose measured width is not 2,
// and any skin tone variant the data might still hold, are left out.
func loadPickerEmoji() {
	pickerByCat = make([][]int, len(emojiCategories))
	pickerIndex = map[string]int{}
	catOf := map[string]int{}
	for i, c := range emojiCategories[1:] {
		catOf[c.name] = i + 1
	}
	cat := 0
	for _, line := range strings.Split(emojiData, "\n") {
		if g, ok := strings.CutPrefix(line, "# "); ok {
			cat = catOf[g]
			continue
		}
		e, name, ok := strings.Cut(line, "\t")
		if !ok || cat == 0 || !pickerUsable(e) {
			continue
		}
		key := emojiKey(e)
		if _, dup := pickerIndex[key]; dup {
			continue
		}
		pickerIndex[key] = len(pickerAll)
		pickerByCat[cat] = append(pickerByCat[cat], len(pickerAll))
		pickerAll = append(pickerAll, pickerEmoji{e: e, name: name, lower: strings.ToLower(name), cat: cat})
	}
}

// pickerUsable reports whether e may be shown in the grid: no skin tone, no
// zero width joiner or keycap, and exactly 2 columns wide.
func pickerUsable(e string) bool {
	if e == "" || dropSkinTones(e) != e || strings.ContainsAny(e, "‍⃣") {
		return false
	}
	return tview.TaggedStringWidth(e) == 2
}

// searchEmoji returns the emoji whose name contains query (case-insensitive),
// names starting with it first, each part in data order.
func searchEmoji(query string) []int {
	pickerOnce.Do(loadPickerEmoji)
	q := strings.ToLower(query)
	var prefix, contains []int
	for i, p := range pickerAll {
		switch {
		case strings.HasPrefix(p.lower, q):
			prefix = append(prefix, i)
		case strings.Contains(p.lower, q):
			contains = append(contains, i)
		}
	}
	return append(prefix, contains...)
}

// Recent emoji; UI goroutine only, except the file write.
var (
	// recentEmojiPath returns the file the Recent tab is kept in; "" keeps
	// nothing. Tests replace it.
	recentEmojiPath = config.GetRecentEmojiFilePath
	recentLoaded    bool
	recentFile      string
	recentList      []string

	recentSaveMu   sync.Mutex
	recentSaveSeq  uint64 // last save started (UI goroutine)
	recentSaved    uint64 // last save written (under recentSaveMu)
	recentSaveWait sync.WaitGroup
)

// addRecent puts e first in list, without duplicates, at most
// emojiRecentMax long. list is not changed.
func addRecent(list []string, e string) []string {
	out := []string{e}
	k := emojiKey(e)
	for _, r := range list {
		if emojiKey(r) != k && len(out) < emojiRecentMax {
			out = append(out, r)
		}
	}
	return out
}

// readRecentEmoji reads the Recent list from path: one emoji per line. Lines
// that are not a picker emoji are skipped; a missing or unreadable file
// gives an empty list.
func readRecentEmoji(path string) []string {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 64*1024 {
		return nil
	}
	pickerOnce.Do(loadPickerEmoji)
	var list []string
	seen := map[string]bool{}
	for _, line := range strings.Split(string(data), "\n") {
		i, ok := pickerIndex[emojiKey(strings.TrimSpace(line))]
		if !ok || seen[pickerAll[i].e] {
			continue
		}
		seen[pickerAll[i].e] = true
		if list = append(list, pickerAll[i].e); len(list) == emojiRecentMax {
			break
		}
	}
	return list
}

// writeRecentEmoji writes list to path through a temporary file.
func writeRecentEmoji(path string, list []string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".recent_emoji-*")
	if err != nil {
		return err
	}
	_, err = tmp.WriteString(strings.Join(list, "\n") + "\n")
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), path)
	}
	if err != nil {
		_ = os.Remove(tmp.Name())
	}
	return err
}

// loadRecent reads the Recent list once.
func loadRecent() {
	if recentLoaded {
		return
	}
	recentLoaded = true
	recentFile = recentEmojiPath()
	recentList = readRecentEmoji(recentFile)
}

// saveRecent writes the Recent list in a goroutine; only the newest list is
// written when saves overlap. Errors are ignored.
func saveRecent() {
	if recentFile == "" {
		return
	}
	recentSaveSeq++
	seq, path, list := recentSaveSeq, recentFile, slices.Clone(recentList)
	recentSaveWait.Add(1)
	go func() {
		defer recentSaveWait.Done()
		recentSaveMu.Lock()
		defer recentSaveMu.Unlock()
		if seq < recentSaved {
			return
		}
		recentSaved = seq
		_ = writeRecentEmoji(path, list)
	}()
}

// insertIntoInput puts s into the input line at the cursor, as a paste does:
// the cursor ends after it, and drafts, edit and reply modes are kept. If
// the field takes no paste (its drop-down is open), s goes at the end.
func insertIntoInput(s string) {
	in := ctx.TextInput
	before := in.GetText()
	in.PasteHandler()(s, func(tview.Primitive) {})
	if in.GetText() == before {
		in.SetText(before + s)
	}
}

// pickEmoji adds e to the input line and to the Recent list.
func pickEmoji(e string) {
	insertIntoInput(e)
	recentList = addRecent(recentList, e)
	saveRecent()
}

// emojiPicker is the picker window. It covers the whole screen so a click
// outside the window can close it; it draws only the window.
type emojiPicker struct {
	*tview.Box
	frame *tview.Box
	cat   int    // tab shown when query is ""
	query string // search text
	sel   int    // selected item
	top   int    // first grid row shown
	items []int  // indices into pickerAll shown now

	tabs []pickerTab // tab positions of the last draw
}

// pickerTab is where a tab was drawn.
type pickerTab struct{ x0, x1, cat int }

// pickerLayout is where the parts of the window go.
type pickerLayout struct {
	bx, by, bw, bh int // window with border
	ix, iy, iw, ih int // inside the border
	cols, rows     int // grid size in cells
	gridX, gridY   int
}

// activeEmojiPicker is the open picker, nil if none.
var activeEmojiPicker *emojiPicker

func newEmojiPicker() *emojiPicker {
	pickerOnce.Do(loadPickerEmoji)
	p := &emojiPicker{Box: tview.NewBox(), frame: tview.NewBox()}
	p.frame.SetBorder(true).SetTitle(" Emoji ")
	p.frame.SetBackgroundColor(tcell.GetColor(config.Config.Colors.Background))
	p.frame.SetBorderColor(tcell.GetColor(config.Config.Colors.Positive))
	p.cat = 1
	if len(recentList) > 0 {
		p.cat = 0
	}
	p.refresh()
	return p
}

// refresh recomputes the items shown; the selection keeps its index.
func (p *emojiPicker) refresh() {
	switch {
	case p.query != "":
		p.items = searchEmoji(p.query)
	case p.cat == 0:
		p.items = p.items[:0:0]
		for _, e := range recentList {
			if i, ok := pickerIndex[emojiKey(e)]; ok {
				p.items = append(p.items, i)
			}
		}
	default:
		p.items = pickerByCat[p.cat]
	}
	p.sel = max(0, min(p.sel, len(p.items)-1))
}

// reset shows the first item of a new list.
func (p *emojiPicker) reset() {
	p.sel, p.top = 0, 0
	p.refresh()
}

// selected returns the selected emoji, nil if the list is empty.
func (p *emojiPicker) selected() *pickerEmoji {
	if p.sel < 0 || p.sel >= len(p.items) {
		return nil
	}
	return &pickerAll[p.items[p.sel]]
}

func (p *emojiPicker) layout() pickerLayout {
	x, y, w, h := p.GetRect()
	var l pickerLayout
	l.bw, l.bh = min(w, emojiPickerMaxWidth), min(h, emojiPickerMaxHeight)
	l.bx, l.by = x+(w-l.bw)/2, y+(h-l.bh)/2
	l.ix, l.iy, l.iw, l.ih = l.bx+1, l.by+1, max(0, l.bw-2), max(0, l.bh-2)
	l.cols = max(1, l.iw/emojiCellWidth)
	l.rows = max(1, l.ih-4) // tabs, search, grid, name, hints
	l.gridX = l.ix + max(0, (l.iw-l.cols*emojiCellWidth)/2)
	l.gridY = l.iy + 2
	return l
}

// scrollToSel keeps the selected row in view.
func (p *emojiPicker) scrollToSel(l pickerLayout) {
	row := p.sel / l.cols
	if row < p.top {
		p.top = row
	}
	if row >= p.top+l.rows {
		p.top = row - l.rows + 1
	}
	p.top = max(0, min(p.top, p.maxTop(l)))
}

func (p *emojiPicker) maxTop(l pickerLayout) int {
	rowsTotal := (len(p.items) + l.cols - 1) / l.cols
	return max(0, rowsTotal-l.rows)
}

// move handles the navigation keys; it reports whether key was one.
func (p *emojiPicker) move(key tcell.Key) bool {
	l := p.layout()
	n := len(p.items)
	page := l.cols * l.rows
	switch key {
	case tcell.KeyLeft:
		if p.sel > 0 {
			p.sel--
		}
	case tcell.KeyRight:
		if p.sel < n-1 {
			p.sel++
		}
	case tcell.KeyUp:
		if p.sel >= l.cols {
			p.sel -= l.cols
		}
	case tcell.KeyDown:
		if (p.sel/l.cols+1)*l.cols < n {
			p.sel = min(p.sel+l.cols, n-1)
		}
	case tcell.KeyPgUp:
		if p.sel-page >= 0 {
			p.sel -= page
		} else {
			p.sel %= l.cols
		}
	case tcell.KeyPgDn:
		if p.sel+page < n {
			p.sel += page
		} else if n > 0 {
			last := (n - 1) / l.cols * l.cols
			p.sel = min(last+p.sel%l.cols, n-1)
		}
	case tcell.KeyHome:
		p.sel = 0
	case tcell.KeyEnd:
		p.sel = max(0, n-1)
	default:
		return false
	}
	p.scrollToSel(l)
	return true
}

// switchTab shows category cat (wrapping round) and clears the search.
func (p *emojiPicker) switchTab(cat int) {
	n := len(emojiCategories)
	p.cat = ((cat % n) + n) % n
	p.query = ""
	p.reset()
}

// pick adds the selected emoji to the input line; the picker stays open.
func (p *emojiPicker) pick() {
	e := p.selected()
	if e == nil {
		return
	}
	pickEmoji(e.e)
	p.refresh() // the Recent tab changes; the selection keeps its place
}

// InputHandler handles the picker's keys.
func (p *emojiPicker) InputHandler() func(event *tcell.EventKey, setFocus func(p tview.Primitive)) {
	return p.WrapInputHandler(func(ev *tcell.EventKey, setFocus func(p tview.Primitive)) {
		if p.move(ev.Key()) {
			return
		}
		switch ev.Key() {
		case tcell.KeyEnter:
			p.pick()
		case tcell.KeyEscape:
			if p.query != "" {
				p.query = ""
				p.reset()
				return
			}
			closeOverlay()
		case tcell.KeyTab:
			if p.query == "" {
				p.switchTab(p.cat + 1)
			}
		case tcell.KeyBacktab:
			if p.query == "" {
				p.switchTab(p.cat - 1)
			}
		case tcell.KeyBackspace, tcell.KeyBackspace2:
			if r := []rune(p.query); len(r) > 0 {
				p.query = string(r[:len(r)-1])
				p.reset()
			}
		case tcell.KeyRune:
			if ev.Modifiers()&(tcell.ModCtrl|tcell.ModAlt) != 0 || ev.Rune() < ' ' {
				return
			}
			if p.query == "" && ev.Rune() == ' ' {
				return
			}
			p.query += string(ev.Rune())
			p.reset()
		}
	})
}

// MouseHandler: a click on a tab switches to it, a click on an emoji adds
// it, the wheel scrolls; a click outside the window closes the picker.
// Every mouse event is taken, so nothing reaches the screen behind.
func (p *emojiPicker) MouseHandler() func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (consumed bool, capture tview.Primitive) {
	return p.WrapMouseHandler(func(action tview.MouseAction, event *tcell.EventMouse, setFocus func(p tview.Primitive)) (bool, tview.Primitive) {
		if !p.InRect(event.Position()) {
			return false, nil
		}
		l := p.layout()
		mx, my := event.Position()
		inside := mx >= l.bx && mx < l.bx+l.bw && my >= l.by && my < l.by+l.bh
		switch action {
		case tview.MouseLeftClick, tview.MouseLeftDoubleClick: // quick clicks add again
			if !inside {
				closeOverlay()
				return true, nil
			}
			setFocus(p)
			if my == l.iy {
				for _, t := range p.tabs {
					if mx >= t.x0 && mx < t.x1 {
						p.switchTab(t.cat)
					}
				}
				return true, nil
			}
			row, col := my-l.gridY, (mx-l.gridX)/emojiCellWidth
			if row >= 0 && row < l.rows && mx >= l.gridX && col < l.cols {
				if i := (p.top+row)*l.cols + col; i < len(p.items) {
					p.sel = i
					p.pick()
				}
			}
		case tview.MouseScrollUp, tview.MouseScrollDown:
			d := 1
			if action == tview.MouseScrollUp {
				d = -1
			}
			p.top = max(0, min(p.top+d, p.maxTop(l)))
			// Keep the selection in view, in its column.
			row, col := p.sel/l.cols, p.sel%l.cols
			row = max(p.top, min(row, p.top+l.rows-1))
			p.sel = max(0, min(row*l.cols+col, len(p.items)-1))
		}
		return true, nil
	})
}

// tabLabels chooses the tab labels that fit width: short names, else
// three-letter names, else three-letter names around the current tab with
// arrows for the hidden ones. It returns the first and last tab shown.
func tabLabels(width, cur int) (labels []string, first, last int) {
	n := len(emojiCategories)
	total := func(get func(emojiCategory) string, a, b int) int {
		w := 0
		for i := a; i <= b; i++ {
			w += len(get(emojiCategories[i]))
		}
		return w + (b - a) // one space between tabs
	}
	short := func(c emojiCategory) string { return c.short }
	tiny := func(c emojiCategory) string { return c.tiny }
	get := short
	first, last = 0, n-1
	if total(short, 0, n-1) > width {
		get = tiny
		if total(tiny, 0, n-1) > width {
			// Grow a window around cur; 2 columns stay for the arrows.
			first, last = cur, cur
			for grown := true; grown; {
				grown = false
				if last < n-1 && total(tiny, first, last+1) <= width-2 {
					last, grown = last+1, true
				}
				if first > 0 && total(tiny, first-1, last) <= width-2 {
					first, grown = first-1, true
				}
			}
		}
	}
	for _, c := range emojiCategories {
		labels = append(labels, get(c))
	}
	return labels, first, last
}

// pickerHints is the key help in the last line, as long as width allows.
func pickerHints(width int) string {
	for _, h := range []string{
		"Enter/click add  Arrows move  Tab category  Type to search  Esc close",
		"Enter/click add  Tab category  Esc close",
		"Enter add  Tab cat  Esc close",
	} {
		if len(h) <= width {
			return h
		}
	}
	return "Esc close"
}

// Draw draws the window: tabs, search line, grid, name and key help.
func (p *emojiPicker) Draw(screen tcell.Screen) {
	l := p.layout()
	if l.iw <= 0 || l.ih <= 0 {
		return
	}
	bg := tcell.GetColor(config.Config.Colors.Background)
	fg := tcell.GetColor(config.Config.Colors.Text)
	accent := tcell.GetColor(config.Config.Colors.Positive)
	dim := tcell.ColorGray
	p.frame.SetRect(l.bx, l.by, l.bw, l.bh)
	p.frame.Draw(screen)
	put := func(text string, x, y, w int, style tcell.Style) {
		for i := 0; i < w; i++ {
			screen.SetContent(x+i, y, ' ', nil, style)
		}
		fgc, _, _ := style.Decompose()
		tview.Print(screen, text, x, y, w, tview.AlignLeft, fgc)
	}
	base := tcell.StyleDefault.Background(bg).Foreground(fg)

	// Tabs; greyed while searching.
	p.tabs = p.tabs[:0]
	labels, first, last := tabLabels(l.iw, p.cat)
	x := l.ix
	if first > 0 {
		put("‹", x, l.iy, 1, base.Foreground(dim))
		x++
	}
	for i := first; i <= last; i++ {
		st := base
		switch {
		case p.query != "":
			st = base.Foreground(dim)
		case i == p.cat:
			st = base.Background(accent).Foreground(bg)
		}
		w := len(labels[i])
		put(labels[i], x, l.iy, w, st)
		p.tabs = append(p.tabs, pickerTab{x, x + w, i})
		x += w + 1
	}
	if last < len(labels)-1 {
		put("›", l.ix+l.iw-1, l.iy, 1, base.Foreground(dim))
	}

	// Search line.
	if p.query == "" {
		put("Search: type a name", l.ix, l.iy+1, l.iw, base.Foreground(dim))
	} else {
		put("Search: "+tview.Escape(p.query)+"_", l.ix, l.iy+1, l.iw, base)
	}

	// Grid.
	p.scrollToSel(l)
	if len(p.items) == 0 {
		msg := "No emoji match"
		if p.query == "" {
			msg = "No recent emoji yet"
		}
		put(msg, l.ix+1, l.gridY, l.iw-1, base.Foreground(dim))
	}
	for r := 0; r < l.rows; r++ {
		for c := 0; c < l.cols; c++ {
			i := (p.top+r)*l.cols + c
			if i >= len(p.items) {
				break
			}
			st := base
			if i == p.sel {
				st = base.Background(accent)
			}
			put(pickerAll[p.items[i]].e, l.gridX+c*emojiCellWidth, l.gridY+r, emojiCellWidth, st)
		}
	}

	// Name of the selected emoji, then the keys.
	name := ""
	if e := p.selected(); e != nil {
		name = e.name
		if p.query == "" {
			name += "  [" + emojiCategories[e.cat].short + "]"
		}
	}
	put(tview.Escape(name), l.ix, l.iy+l.ih-2, l.iw, base)
	put(pickerHints(l.iw), l.ix, l.iy+l.ih-1, l.iw, base.Foreground(dim))
}

// emojiToggle holds the emoji_picker key; while the picker is open, it
// closes it (globalKeys).
var emojiToggle *cbind.Configuration

// setEmojiPickerKey binds key to open the picker (in the global key map, if
// given) and to close it while it is open.
func setEmojiPickerKey(global *cbind.Configuration, key string) error {
	c := cbind.NewConfiguration()
	if err := c.Set(key, func(*tcell.EventKey) *tcell.EventKey { closeOverlay(); return nil }); err != nil {
		return err
	}
	emojiToggle = c
	if global != nil {
		return global.Set(key, handleEmojiPicker)
	}
	return nil
}

// handleEmojiPicker opens the picker (the emoji_picker key).
func handleEmojiPicker(ev *tcell.EventKey) *tcell.EventKey {
	openEmojiPicker(false)
	return nil
}

// emojiPickerKeys handles a key while the picker is open: the emoji_picker
// key closes it, everything else goes to the picker.
func emojiPickerKeys(ev *tcell.EventKey) *tcell.EventKey {
	if emojiToggle != nil && emojiToggle.Capture(ev) == nil {
		return nil
	}
	return ev
}

// openEmojiPicker opens the picker over the main screen. fromCommand is set
// for /emoji: entering the command emptied the input line, so the open
// chat's draft comes back first. UI goroutine only.
func openEmojiPicker(fromCommand bool) {
	if pages == nil || ctx.TextInput == nil || overlayName == emojiOverlay {
		return
	}
	if overlayName != "" {
		pages.RemovePage(overlayName) // the help window gives way
		overlayView = nil
	}
	if fromCommand && editing == nil && ctx.TextInput.GetText() == "" {
		if d := drafts[ctx.CurrentReceiver.Id]; d != "" {
			setInputText(d)
		}
	}
	loadRecent()
	p := newEmojiPicker()
	activeEmojiPicker = p
	overlayName, overlayFocus = emojiOverlay, ctx.TextInput
	pages.AddPage(emojiOverlay, p, true, true)
	ctx.App.SetFocus(p)
}
