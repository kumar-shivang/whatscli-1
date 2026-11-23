# Emoji picker: design

_Last updated: 2026-11-23T09:58:02+00:00 (implemented on branch `feature/emoji-picker`)._

## Why

The only way to add an emoji today is the `:name` drop-down, which needs you to know the name. The owner wants a picker like WhatsApp's: browse by category, search by name, add several emoji in a row, and only the default look of each emoji (no skin tones).

## Behaviour

**Table EP1: Picker behaviour**

| Area | Behaviour |
|---|---|
| Open | Key `emoji_picker` (default `Ctrl+t`, free in the default keymap) or `/emoji`; an overlay over the main screen. `/emoji` brings back the chat's draft that the command line hid |
| Layout | Tab row, search line, grid, name of the selected emoji, key hints (`Enter/click add ... Esc close`) |
| Tabs | Recent, Smileys and Emotion, People and Body, Animals and Nature, Food and Drink, Travel and Places, Activities, Objects, Symbols, Flags. Short labels; three-letter labels and a scrolling window with arrows when narrow. Greyed while searching |
| Grid | 3 columns per cell; column count follows the width (window at most 80 by 22; about 40 columns is the least usable) |
| Keys | Arrows move (`Left`/`Right` wrap to the previous or next row, `Up`/`Down` clamp), `Home`/`End`, `PgUp`/`PgDn` a page, `Tab`/`Shift+Tab` category, typing edits the search, `Backspace` deletes |
| Search | Case-insensitive substring of the CLDR name across all categories; names that start with the query first, each part in CLDR order |
| Pick | `Enter` or a click inserts the emoji at the input cursor. The picker stays open; the cursor moves past each pick, so several in a row land in order. Nothing is sent; drafts, `edit>` and reply modes are kept |
| Close | `Esc` clears a search, else closes; the `emoji_picker` key again; a click outside the window. Focus returns to the input line |
| Mouse | Click a tab to switch, click an emoji to add it (quick repeated clicks count as picks), wheel scrolls |
| Recent | Last 24 picks, newest first, updated live (the selection keeps its place). Kept in `<XDG_STATE_HOME>/whatscli/recent_emoji`, one emoji per line; read and write errors are ignored; lines that are not a known emoji are skipped |

## Decisions

- Data: Unicode `emoji-test.txt` (Emoji 18.0), fully-qualified lines only, the Component group skipped, turned into `emoji_data.go` by `tools/gen-emoji-data` (standard library only). About 31 KB of source, about 87 KB of binary growth.
- Left out by the generator: skin tone modifiers (U+1F3FB to U+1F3FF), ZWJ sequences (U+200D), keycaps (U+20E3) and tag sequences (the England, Scotland and Wales flags). Regional-indicator flags stay. One entry per emoji, ignoring U+FE0F.
- Left out at load time: any emoji whose width (`tview.TaggedStringWidth`, uniseg tables) is not 2. With uniseg v0.4.7 that drops 23 emoji, all from Emoji 16.0 and later, which most terminal fonts cannot draw yet.
- Insertion uses the input field's paste handler, so the text lands at the cursor and the field's changed function runs as for typing. If the field refuses the paste (its drop-down is open), the emoji is appended.
- The picker is a page in the existing overlay mechanism (`overlayName`, `closeOverlay`); it covers the whole screen so a click outside its window can close it.
- Data is parsed on first open (`sync.Once`); the Recent file is written in a goroutine, newest list wins.
- `/emoji` follows the `/style` pattern: a backend command that calls a new `UiMessageHandler.OpenEmojiPicker` (23 methods).

## Non-goals

- Skin tones, ZWJ sequences (families, couples with skin tones, professions), keycaps.
- Custom emoji, stickers, GIF panels.
- Search keywords beyond the CLDR name (no CLDR annotation keywords, no other languages).

## Open

- [Unverified] Terminals that draw U+FE0F emoji (red heart) one column wide would shift a grid row. ROADMAP row 40 and TESTING step 36 track a check in real terminals.
