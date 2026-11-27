package app

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestShiftMouseYMovesEveryMouseMessage(t *testing.T) {
	tests := []struct {
		name string
		msg  tea.MouseMsg
		want int
	}{
		{"click", tea.MouseClickMsg{X: 3, Y: 10, Button: tea.MouseLeft}, 7},
		{"release", tea.MouseReleaseMsg{X: 3, Y: 10, Button: tea.MouseLeft}, 7},
		{"wheel", tea.MouseWheelMsg{X: 3, Y: 10, Button: tea.MouseWheelDown}, 7},
		{"motion", tea.MouseMotionMsg{X: 3, Y: 10}, 7},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shiftMouseY(tt.msg, -3)
			if y := got.Mouse().Y; y != tt.want {
				t.Errorf("shifted Y = %d, want %d", y, tt.want)
			}
		})
	}
}

func TestShiftMouseYWithoutShiftReturnsTheMessage(t *testing.T) {
	msg := tea.MouseClickMsg{X: 3, Y: 10, Button: tea.MouseLeft}
	if got := shiftMouseY(msg, 0); got != msg {
		t.Errorf("a zero shift returned %v, want the message unchanged", got)
	}
}

func TestAMouseClickOffTheDetailScreenIsSwallowed(t *testing.T) {
	m := newInternal(Mock{})
	m.width, m.height = 160, 44
	if m.screen == screenDetail {
		t.Fatal("the check is broken: the host opened on the detail screen")
	}

	model, cmd := m.handleMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	if cmd != nil {
		t.Error("a click off the detail screen produced a command")
	}
	after, ok := model.(Model)
	if !ok {
		t.Fatal("the model changed type")
	}
	if after.screen != m.screen {
		t.Error("a click off the detail screen changed the screen")
	}
}

func TestAMouseClickBelowTheMinimumSizeIsSwallowed(t *testing.T) {
	m := onADetail(t)
	m.width, m.height = minWidth-1, minHeight-1

	_, cmd := m.handleMouse(tea.MouseClickMsg{X: 5, Y: 5, Button: tea.MouseLeft})
	if cmd != nil {
		t.Error("a click below the minimum size produced a command")
	}
}

func TestAMouseClickAboveTheBodyIsSwallowed(t *testing.T) {
	m := onADetail(t)

	_, cmd := m.handleMouse(tea.MouseClickMsg{X: 5, Y: -1, Button: tea.MouseLeft})
	if cmd != nil {
		t.Error("a click above the body produced a command")
	}
}
