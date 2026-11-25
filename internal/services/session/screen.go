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
	if w < 1 || h < 1 || w > 65535 || h > 65535 {
		return nil, fmt.Errorf("invalid terminal size %dx%d", w, h)
	}
	v, err := getViewer(pod, term, w, h)
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
