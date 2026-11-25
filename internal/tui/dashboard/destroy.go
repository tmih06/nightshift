package dashboard

import (
	"context"

	tea "charm.land/bubbletea/v2"
	"github.com/tmih06/nightshift/internal/tui/dialog"
)

// destroyDialog asks, on x over a sandbox on a pull request's volume,
// whether the volume goes too. Keeping it is the first choice.
type destroyDialog struct {
	pod, volume string
	cursor      int
}

const (
	keepVolume = iota
	dropVolume
)

func (d destroyDialog) menu() dialog.Menu {
	return dialog.Menu{
		Title: "Destroy sandbox " + d.pod + "?",
		Lines: []string{"Volume " + d.volume + " holds the clone and any work in it."},
		Options: []dialog.Option{
			keepVolume: {Text: " 1  Destroy the sandbox, keep the volume "},
			dropVolume: {Text: " 2  Destroy the sandbox and the volume ", Danger: true},
		},
		Cursor: d.cursor,
		Hint:   "↑/↓ choose · enter confirm · esc cancel",
	}
}

func (m model) destroyPlaced() dialog.Placed {
	return m.destroy.menu().Place(th, m.width, m.height)
}

func (m model) updateDestroyKeys(msg tea.KeyPressMsg) (model, tea.Cmd) {
	d := *m.destroy
	menu := d.menu()
	switch msg.String() {
	case "esc", "q", "n", "ctrl+c":
		m.destroy = nil
		m.flash("kept " + d.pod)
		return m, nil
	case "up", "k", "shift+tab":
		menu.Move(-1)
	case "down", "j", "tab":
		menu.Move(1)
	case "enter":
		return m.destroyChoice(d.cursor)
	case "1":
		return m.destroyChoice(keepVolume)
	case "2":
		return m.destroyChoice(dropVolume)
	}
	d.cursor = menu.Cursor
	m.destroy = &d
	return m, nil
}

func (m model) destroyMouse(msg tea.MouseMsg) (model, tea.Cmd) {
	click, ok := msg.(tea.MouseClickMsg)
	if !ok || click.Button != tea.MouseLeft {
		return m, nil
	}
	p := m.destroyPlaced()
	if !p.Inside(click.X, click.Y) {
		m.destroy = nil
		return m, nil
	}
	if i, ok := p.OptionAt(click.X, click.Y); ok {
		return m.destroyChoice(i)
	}
	return m, nil
}

func (m model) destroyChoice(choice int) (model, tea.Cmd) {
	d := *m.destroy
	m.destroy = nil
	drop := choice == dropVolume
	if drop {
		m.sticky("destroying " + d.pod + " and volume " + d.volume + "…")
	} else {
		m.sticky("destroying " + d.pod + ", keeping volume " + d.volume + "…")
	}
	return m, deleteSandboxCmd(m.api, d.pod, d.volume, drop)
}

func deleteSandboxCmd(api API, name, volume string, dropVolume bool) tea.Cmd {
	return func() tea.Msg {
		_, err := call(callTimeout, func(ctx context.Context) (struct{}, error) {
			return struct{}{}, api.DeleteSandbox(ctx, name, dropVolume)
		})
		if err != nil {
			return deletedMsg{sandbox: name, volume: volume, err: err}
		}
		return deletedMsg{sandbox: name, volume: volume, volumeGone: dropVolume}
	}
}
