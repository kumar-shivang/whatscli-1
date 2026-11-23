// SPDX-License-Identifier: AGPL-3.0-only

package main

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/gdamore/tcell/v2"
)

// focusRecorder collects the values focusScreen reports through onFocus.
type focusRecorder struct {
	mu  sync.Mutex
	got []bool
}

func (r *focusRecorder) record(f bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, f)
}

func (r *focusRecorder) values() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]bool(nil), r.got...)
}

// pollWithin runs PollEvent and fails the test if it does not return in time.
func pollWithin(t *testing.T, s tcell.Screen) tcell.Event {
	t.Helper()
	ch := make(chan tcell.Event, 1)
	go func() { ch <- s.PollEvent() }()
	select {
	case ev := <-ch:
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("PollEvent did not return")
		return nil
	}
}

func TestFocusScreenReportsAndSwallowsFocus(t *testing.T) {
	rec := &focusRecorder{}
	s := &focusScreen{Screen: tcell.NewSimulationScreen(""), onFocus: rec.record}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer s.Fini()

	key1 := tcell.NewEventKey(tcell.KeyRune, 'a', tcell.ModNone)
	for _, ev := range []tcell.Event{tcell.NewEventFocus(false), key1, tcell.NewEventFocus(true)} {
		if err := s.PostEvent(ev); err != nil {
			t.Fatalf("PostEvent: %v", err)
		}
	}

	ev := pollWithin(t, s)
	if ev != key1 {
		t.Fatalf("first event = %#v, want the first key event", ev)
	}
	if got := rec.values(); len(got) != 1 || got[0] != false {
		t.Fatalf("onFocus calls after first poll = %v, want [false]", got)
	}

	key2 := tcell.NewEventKey(tcell.KeyRune, 'b', tcell.ModNone)
	if err := s.PostEvent(key2); err != nil {
		t.Fatalf("PostEvent: %v", err)
	}
	ev = pollWithin(t, s)
	if ev != key2 {
		t.Fatalf("second event = %#v, want the second key event", ev)
	}
	if got := rec.values(); len(got) != 2 || got[0] != false || got[1] != true {
		t.Fatalf("onFocus calls = %v, want [false true]", got)
	}
}

func TestFocusScreenPassesNil(t *testing.T) {
	rec := &focusRecorder{}
	s := &focusScreen{Screen: tcell.NewSimulationScreen(""), onFocus: rec.record}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	s.Fini()
	if ev := pollWithin(t, s); ev != nil {
		t.Fatalf("PollEvent after Fini = %#v, want nil", ev)
	}
	if got := rec.values(); len(got) != 0 {
		t.Fatalf("onFocus calls = %v, want none", got)
	}
}

// fakeInnerScreen records the calls focusScreen makes on its inner screen.
// Methods it does not override panic through the nil embedded Screen.
type fakeInnerScreen struct {
	tcell.Screen
	initErr error
	calls   []string
}

func (f *fakeInnerScreen) Init() error   { f.calls = append(f.calls, "Init"); return f.initErr }
func (f *fakeInnerScreen) Resume() error { f.calls = append(f.calls, "Resume"); return nil }
func (f *fakeInnerScreen) EnableFocus()  { f.calls = append(f.calls, "EnableFocus") }

func TestFocusScreenEnablesFocusOnInitAndResume(t *testing.T) {
	inner := &fakeInnerScreen{}
	s := &focusScreen{Screen: inner}
	if err := s.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := s.Resume(); err != nil {
		t.Fatalf("Resume: %v", err)
	}
	want := []string{"Init", "EnableFocus", "Resume", "EnableFocus"}
	if len(inner.calls) != len(want) {
		t.Fatalf("calls = %v, want %v", inner.calls, want)
	}
	for i := range want {
		if inner.calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", inner.calls, want)
		}
	}
	if s.initErr != nil {
		t.Fatalf("initErr = %v, want nil", s.initErr)
	}
}

func TestFocusScreenKeepsInitError(t *testing.T) {
	boom := errors.New("no tty")
	inner := &fakeInnerScreen{initErr: boom}
	s := &focusScreen{Screen: inner}
	if err := s.Init(); err != boom {
		t.Fatalf("Init = %v, want %v", err, boom)
	}
	if s.initErr != boom {
		t.Fatalf("initErr = %v, want %v", s.initErr, boom)
	}
	if len(inner.calls) != 1 {
		t.Fatalf("calls = %v, want only Init (no EnableFocus after a failed Init)", inner.calls)
	}
}
