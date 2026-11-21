package session

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/vt"
)

// BenchmarkShellScreen measures terminal output processing and a screen read,
// excluding the kubectl tunnel and dashboard rendering.
func BenchmarkShellScreen(b *testing.B) {
	const pod = "benchmark"
	term := Name(pod)
	v := &viewer{term: term, dirty: make(chan struct{}, 1), done: make(chan struct{}), screen: vt.NewEmulator(80, 24)}
	v.screen.SetScrollbackSize(1)
	v.drainScreenReplies()
	defer v.closeScreen()
	c := &podConn{
		pod:      pod,
		done:     make(chan struct{}),
		viewers:  map[string]*viewer{term: v},
		channels: map[uint32]*viewer{1: v},
	}
	v.conn = c
	conns.Lock()
	conns.byPod[pod] = c
	conns.Unlock()
	defer func() {
		conns.Lock()
		delete(conns.byPod, pod)
		conns.Unlock()
	}()
	v.applyOutput([]byte(strings.Repeat("\x1b[32mnode@pod\x1b[0m:~/workspace$ ls -la\r\n", 24)))
	<-v.dirty
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{"keystroke", "\x1b[1;1Hx"},
		{"redraw", "\x1b[H" + strings.Repeat("\x1b[38;2;12;34;56mcolored\x1b[0m shell output\r\n", 24)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			payload := []byte(tc.payload)
			b.ReportAllocs()
			for b.Loop() {
				v.applyOutput(payload)
				if _, err := WatchScreen(pod, term, 80, 24, false, time.Second); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
