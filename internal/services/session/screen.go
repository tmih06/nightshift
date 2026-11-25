package session

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// WatchScreen waits for output on one terminal inside pod, then renders the
// viewer's terminal screen. A nil screen means nothing changed before wait
// elapsed.
func WatchScreen(pod, term string, w, h int, resize bool, wait time.Duration) ([]string, error) {
	return WatchScreenFor(Watch{Pod: pod, Term: term, W: w, H: h, Resize: resize, Wait: wait})
}

// Watch is what one viewer asks of a terminal's screen. Each Viewer sees its
// own changes, so frontends on different machines can watch the same terminal,
// each at its own size; "" is the shared viewer that input goes through.
type Watch struct {
	Pod, Term, Viewer string
	W, H              int
	Resize            bool
	Wait              time.Duration
}

// WatchScreenFor is WatchScreen for one viewer among several.
func WatchScreenFor(r Watch) ([]string, error) {
	pod, term, who, w, h, resize, wait := r.Pod, r.Term, r.Viewer, r.W, r.H, r.Resize, r.Wait
	if w < 1 || h < 1 || w > 65535 || h > 65535 {
		return nil, fmt.Errorf("invalid terminal size %dx%d", w, h)
	}
	v, err := getViewerAs(pod, term, who, w, h)
	if err != nil {
		return nil, err
	}
	if resize {
		if err := v.resize(w, h); err != nil {
			return nil, err
		}
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-v.dirty:
		case <-v.done:
			return nil, errors.New("hc viewer closed")
		case <-timer.C:
			return nil, nil
		}
	}
	v.screenMu.Lock()
	defer v.screenMu.Unlock()
	return strings.Split(v.screen.Render(), "\n"), nil
}
