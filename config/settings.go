// SPDX-License-Identifier: AGPL-3.0-only

package config

import (
	"os"
	"os/user"

	"github.com/adrg/xdg"
	"gopkg.in/ini.v1"
)

var configFilePath string
var cfg *ini.File

type IniFile struct {
	*General
	*Keymap
	*Ui
	*Colors
}

type General struct {
	DownloadPath        string
	PreviewPath         string
	CmdPrefix           string
	ShowCommand         string
	EnableNotifications bool
	UseTerminalBell     bool
	PlaySound           bool   // play a sound on notification
	NotificationSound   string // path to a custom sound file; empty = built-in
	NotificationTimeout int64
	BacklogMsgQuantity  int
	UploadSearchPath    string // folder the file picker searches
	ImageExtensions     string // comma-separated; files /sendimage offers
	VideoExtensions     string // comma-separated; files /sendvideo offers
	AudioExtensions     string // comma-separated; files /sendaudio offers
	SharePresence       bool   // online while active, typing sent and shown (messages/presence.go)
}

type Keymap struct {
	SwitchPanels    string
	FocusMessages   string
	FocusInput      string
	FocusChats      string
	FindChat        string
	Copyuser        string
	Pasteuser       string
	CommandBacklog  string
	CommandRead     string
	CommandConnect  string
	CommandQuit     string
	CommandHelp     string
	MessageDownload string
	MessageOpen     string
	MessageShow     string
	MessageUrl      string
	MessageInfo     string
	MessageRevoke   string
	PickFile        string
	MessageRetry    string
	MessageQuote    string
	EmojiPicker     string // opens and closes the emoji picker (ui_emoji_picker.go)
}

type Ui struct {
	ChatSidebarWidth int
	MessageStyle     string // "bubbles" (default) or "classic" (one line per message)
}

type Colors struct {
	Background      string
	Text            string
	ForwardedText   string
	ListHeader      string
	ListContact     string
	ListGroup       string
	ChatContact     string
	ChatMe          string
	Borders         string
	InputBackground string
	InputText       string
	UnreadCount     string
	Positive        string
	Negative        string
	BubbleMe        string
	BubbleContact   string
	BubbleTime      string
	StatusRead      string
}

var Config = IniFile{
	&General{
		DownloadPath:        GetHomeDir() + "Downloads",
		PreviewPath:         GetHomeDir() + "Downloads",
		CmdPrefix:           "/",
		ShowCommand:         "jp2a --color",
		EnableNotifications: true,
		UseTerminalBell:     false,
		PlaySound:           true,
		NotificationSound:   "",
		NotificationTimeout: 60,
		BacklogMsgQuantity:  10,
		UploadSearchPath:    GetHomeDir(),
		ImageExtensions:     "jpg,jpeg,png,webp",
		VideoExtensions:     "mp4,3gp,mov,mkv,webm",
		AudioExtensions:     "mp3,ogg,opus,m4a,aac,amr,wav",
		SharePresence:       true,
	},
	&Keymap{
		SwitchPanels:    "Tab",
		FocusMessages:   "Ctrl+w",
		FocusInput:      "Ctrl+Space",
		FocusChats:      "Ctrl+e",
		FindChat:        "Ctrl+f",
		CommandBacklog:  "Ctrl+b",
		CommandRead:     "Ctrl+n",
		Copyuser:        "Ctrl+c",
		Pasteuser:       "Ctrl+v",
		CommandConnect:  "Ctrl+r",
		CommandQuit:     "Ctrl+q",
		CommandHelp:     "Ctrl+g",
		MessageDownload: "d",
		MessageInfo:     "i",
		MessageOpen:     "o",
		MessageUrl:      "u",
		MessageRevoke:   "r",
		MessageShow:     "s",
		PickFile:        "Ctrl+o",
		MessageRetry:    "t",
		MessageQuote:    "q",
		EmojiPicker:     "Ctrl+t",
	},
	&Ui{
		ChatSidebarWidth: 30,
		MessageStyle:     "bubbles",
	},
	&Colors{
		Background:      "black",
		Text:            "white",
		ForwardedText:   "purple",
		ListHeader:      "yellow",
		ListContact:     "green",
		ListGroup:       "blue",
		ChatContact:     "green",
		ChatMe:          "blue",
		Borders:         "white",
		InputBackground: "blue",
		InputText:       "white",
		UnreadCount:     "yellow",
		Positive:        "green",
		Negative:        "red",
		BubbleMe:        "#005c4b",
		BubbleContact:   "#202c33",
		BubbleTime:      "silver",
		StatusRead:      "#53bdeb",
	},
}

func InitConfig() error {
	var err error
	if configFilePath, err = xdg.ConfigFile("whatscli/whatscli.config"); err == nil {
		// add any new values
		var cfg *ini.File
		if cfg, err = ini.Load(configFilePath); err == nil {
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if section, err := cfg.GetSection("general"); err == nil {
				section.MapTo(&Config.General)
			}
			if section, err := cfg.GetSection("keymap"); err == nil {
				section.MapTo(&Config.Keymap)
			}
			if section, err := cfg.GetSection("ui"); err == nil {
				section.MapTo(&Config.Ui)
			}
			if section, err := cfg.GetSection("colors"); err == nil {
				section.MapTo(&Config.Colors)
			}
		} else {
			cfg = ini.Empty()
			cfg.NameMapper = ini.TitleUnderscore
			cfg.ValueMapper = os.ExpandEnv
			if err = ini.ReflectFromWithMapper(cfg, &Config, ini.TitleUnderscore); err == nil {
				err = cfg.SaveTo(configFilePath)
			}
		}
	}
	return err
}

func GetConfigFilePath() string {
	return configFilePath
}

func GetSessionFilePath() string {
	if sessionFilePath, err := xdg.ConfigFile("whatscli/session"); err == nil {
		return sessionFilePath
	}
	return GetHomeDir() + ".whatscli.session"
}

// GetRecentEmojiFilePath returns the file that keeps the emoji picker's
// recently used emoji (<XDG_STATE_HOME>/whatscli/recent_emoji), or "" when no
// state directory can be found or made; the list is then not kept.
func GetRecentEmojiFilePath() string {
	p, err := xdg.StateFile("whatscli/recent_emoji")
	if err != nil {
		return ""
	}
	return p
}

// gets the OS home dir with a path separator at the end
func GetHomeDir() string {
	usr, err := user.Current()
	if err == nil {
		return usr.HomeDir + string(os.PathSeparator)
	}
	// Fallback to environment variable
	home := os.Getenv("HOME")
	if home != "" {
		return home + string(os.PathSeparator)
	}
	return "." + string(os.PathSeparator)
}
