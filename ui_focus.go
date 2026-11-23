// SPDX-License-Identifier: AGPL-3.0-only

package main

import "github.com/gdamore/tcell/v2"

// focusScreen wraps a tcell.Screen to turn on terminal focus reporting and
// hand focus changes to onFocus. tview v0.42 reads events only through
// PollEvent and ignores *tcell.EventFocus, so the wrapper consumes those
// events itself and passes every other event through unchanged.
type focusScreen struct {
	tcell.Screen
	// onFocus is called with true when the terminal gains focus and false
	// when it loses it. It runs on tview's event-reading goroutine and must
	// not block.
	onFocus func(bool)
	// initErr is the error from the last Init. tview's SetScreen calls Init
	// and discards its error, so main reads it from here.
	initErr error
}

// Init initialises the inner screen and enables focus reporting. The error
// is also kept in initErr.
func (s *focusScreen) Init() error {
	s.initErr = s.Screen.Init()
	if s.initErr != nil {
		return s.initErr
	}
	s.Screen.EnableFocus()
	return nil
}

// Resume resumes the inner screen and re-enables focus reporting, which
// Suspend turned off (tview's Application.Suspend, used by the fzf picker).
func (s *focusScreen) Resume() error {
	if err := s.Screen.Resume(); err != nil {
		return err
	}
	s.Screen.EnableFocus()
	return nil
}

// PollEvent returns the next event that is not a focus event. Focus events
// are reported to onFocus and skipped. A nil event (screen finalised) is
// returned as is.
func (s *focusScreen) PollEvent() tcell.Event {
	for {
		ev := s.Screen.PollEvent()
		if fe, ok := ev.(*tcell.EventFocus); ok {
			if s.onFocus != nil {
				s.onFocus(fe.Focused)
			}
			continue
		}
		return ev
	}
}
