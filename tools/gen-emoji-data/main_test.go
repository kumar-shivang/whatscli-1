// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"strings"
	"testing"
)

const sample = `# Version: 99.0
# Date: 2099-01-01
# group: Smileys & Emotion
1F600 ; fully-qualified # 😀 E1.0 grinning face
263A FE0F ; fully-qualified # ☺️ E0.6 smiling face
263A ; unqualified # ☺ E0.6 smiling face
1F44B 1F3FD ; fully-qualified # 👋🏽 E1.0 waving hand: medium skin tone
1F636 200D 1F32B FE0F ; fully-qualified # 😶‍🌫️ E13.1 face in clouds
0023 FE0F 20E3 ; fully-qualified # #️⃣ E0.6 keycap: #
1F601 ; fully-qualified # 😁 E1.0 bad` + "`" + `name
ZZZZ ; fully-qualified # ? E1.0 broken
# group: People & Body
1F44B ; fully-qualified # 👋 E0.6 waving hand
# group: Component
1F3FB ; component # 🏻 E1.0 light skin tone
# group: Animals & Nature
1F436 ; fully-qualified # 🐶 E0.6 dog face
# group: Food & Drink
1F355 ; fully-qualified # 🍕 E0.6 pizza
# group: Travel & Places
1F697 ; fully-qualified # 🚗 E0.6 automobile
# group: Activities
26BD ; fully-qualified # ⚽ E0.6 soccer ball
# group: Objects
1F4A1 ; fully-qualified # 💡 E0.6 light bulb
# group: Symbols
2705 ; fully-qualified # ✅ E0.6 check mark button
# group: Flags
1F1EE 1F1F3 ; fully-qualified # 🇮🇳 E2.0 flag: India
1F3F4 E0067 E0062 E0065 E006E E0067 E007F ; fully-qualified # 🏴󠁧󠁢󠁥󠁮󠁧󠁿 E5.0 flag: England
`

func TestGenerate(t *testing.T) {
	out, err := generate(strings.NewReader(sample))
	if err != nil {
		t.Fatal(err)
	}
	src := string(out)
	if !strings.HasPrefix(src, "// SPDX-License-Identifier: AGPL-3.0-only\n") || !strings.Contains(src, "DO NOT EDIT") {
		t.Fatalf("header missing:\n%s", src)
	}
	for _, want := range []string{`emojiDataVersion = "99.0"`, "😀\tgrinning face", "☺️\tsmiling face", "👋\twaving hand", "# Symbols\n"} {
		if !strings.Contains(src, want) {
			t.Errorf("output lacks %q", want)
		}
	}
	for _, bad := range []string{"🏽", "‍", "⃣", "England", "India", "# Flags", ": medium skin tone", "broken", "bad"} {
		if strings.Contains(src, bad) {
			t.Errorf("output contains %q", bad)
		}
	}
	if strings.Count(src, "smiling face") != 1 {
		t.Error("smiling face listed twice")
	}
	if _, err := generate(strings.NewReader("# group: Symbols\n")); err == nil {
		t.Error("input without a version should fail")
	}
}
