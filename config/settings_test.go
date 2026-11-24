// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/ini.v1"
)

func TestDefaults_CmdPrefix(t *testing.T) {
	if Config.General.CmdPrefix != "/" {
		t.Errorf("expected default CmdPrefix '/', got %q", Config.General.CmdPrefix)
	}
}

func TestDefaults_DownloadPath(t *testing.T) {
	if !strings.HasSuffix(Config.General.DownloadPath, "Downloads") {
		t.Errorf("expected DownloadPath to end with 'Downloads', got %q", Config.General.DownloadPath)
	}
}

func TestDefaults_KeyBindings(t *testing.T) {
	tests := []struct {
		name     string
		got      string
		expected string
	}{
		{"SwitchPanels", Config.Keymap.SwitchPanels, "Tab"},
		{"FocusInput", Config.Keymap.FocusInput, "Ctrl+Space"},
		{"CommandQuit", Config.Keymap.CommandQuit, "Ctrl+q"},
		{"CommandHelp", Config.Keymap.CommandHelp, "Ctrl+g"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, tt.got)
			}
		})
	}
}

func TestDefaults_Colors(t *testing.T) {
	if Config.Colors.Background != "black" {
		t.Errorf("expected default Background 'black', got %q", Config.Colors.Background)
	}
	if Config.Colors.Positive != "green" {
		t.Errorf("expected default Positive 'green', got %q", Config.Colors.Positive)
	}
}

func TestDefaults_Ui(t *testing.T) {
	if Config.Ui.ChatSidebarWidth != 30 {
		t.Errorf("expected default ChatSidebarWidth 30, got %d", Config.Ui.ChatSidebarWidth)
	}
}

func TestGetHomeDir(t *testing.T) {
	home := GetHomeDir()
	if home == "" {
		t.Error("GetHomeDir returned empty string")
	}
	if !strings.HasSuffix(home, string(os.PathSeparator)) {
		t.Errorf("GetHomeDir should end with path separator, got %q", home)
	}
}

func TestGetSessionFilePath(t *testing.T) {
	path := GetSessionFilePath()
	if path == "" {
		t.Error("GetSessionFilePath returned empty string")
	}
}

func TestConfigNotificationDefaults(t *testing.T) {
	g := Config.General
	if !g.EnableNotifications || !g.PlaySound || g.NotificationSound != "" || g.NotificationTimeout != 60 {
		t.Errorf("unexpected defaults: %+v", g)
	}

	f, err := ini.Load([]byte("[general]\nplay_sound = false\nnotification_sound = /tmp/x.wav\n"))
	if err != nil {
		t.Fatal(err)
	}
	f.NameMapper = ini.TitleUnderscore
	var got General
	if err := f.Section("general").MapTo(&got); err != nil {
		t.Fatal(err)
	}
	if got.PlaySound || got.NotificationSound != "/tmp/x.wav" {
		t.Errorf("ini not mapped: %+v", got)
	}
}

func TestApplyVscodeMode(t *testing.T) {
	saved := *Config.Keymap
	savedUi := Config.Ui.VscodeMode
	defer func() { *Config.Keymap = saved; Config.Ui.VscodeMode = savedUi }()

	*Config.Keymap = *defaultKeymap()
	Config.Keymap.FindChat = "Ctrl+k" // user customised: must survive
	ApplyVscodeMode()

	km := Config.Keymap
	if km.FindChat != "Ctrl+k" {
		t.Errorf("custom FindChat overwritten: %q", km.FindChat)
	}
	if km.CommandQuit != "Alt+q" || km.ToggleMouse != "Alt+x" || km.CommandHelp != "Alt+h" {
		t.Errorf("preset not applied: %+v", km)
	}
	if km.SwitchPanels != "Tab" || km.MessageDownload != "d" {
		t.Errorf("unrelated keys changed: %+v", km)
	}

	// no two global bindings collide, and none is a Ctrl key VS Code takes
	seen := map[string]string{}
	v := reflect.ValueOf(VscodeKeymap()).Elem()
	for i := 0; i < v.NumField(); i++ {
		name, key := v.Type().Field(i).Name, v.Field(i).String()
		if strings.HasPrefix(name, "Message") {
			continue // plain letters in message mode
		}
		if prev, dup := seen[key]; dup {
			t.Errorf("%s and %s share %q", prev, name, key)
		}
		seen[key] = name
		if strings.HasPrefix(key, "Ctrl+") {
			t.Errorf("%s still uses %s", name, key)
		}
	}
}
