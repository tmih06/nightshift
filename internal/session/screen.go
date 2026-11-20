package session

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// WatchScreen waits for output, then reads the authoritative screen over the
// same tunnel as the input. A nil screen means nothing changed before wait
// elapsed; it lets the caller recheck visibility and pane size without an RPC.
func WatchScreen(pod string, w, h int, resize bool, wait time.Duration) ([]string, error) {
	if w < 1 || h < 1 || w > 65535 || h > 65535 {
		return nil, fmt.Errorf("invalid terminal size %dx%d", w, h)
	}
	v, err := getViewer(pod, w, h)
	if err != nil {
		return nil, err
	}
	if resize {
		if err := v.resize(w, h); err != nil {
			return nil, err
		}
		select {
		case <-v.dirty:
		default:
		}
	} else {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-v.dirty:
		case <-v.done:
			return nil, fmt.Errorf("hc viewer closed")
		case <-timer.C:
			return nil, nil
		}
	}
	return v.capture(pod)
}

// Only one terminal.read can be in flight per viewer. ID 3 follows hello
// and attach; a timed-out viewer is closed rather than reusing its ID.
const screenRequestID = 3

type screenResponse struct {
	ID     int `json:"id"`
	Result *struct {
		Text string `json:"text"`
	} `json:"result"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (v *viewer) markDirty() {
	select {
	case v.dirty <- struct{}{}:
	default:
	}
}

func (v *viewer) receiveScreen(payload []byte) {
	var reply screenResponse
	if json.Unmarshal(payload, &reply) == nil && reply.ID == screenRequestID {
		select {
		case v.screenReply <- reply:
		default:
		}
	}
}

func (v *viewer) capture(pod string) ([]string, error) {
	v.screenMu.Lock()
	defer v.screenMu.Unlock()

	request := fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%d,"method":"terminal.read","params":{"terminal":%q}}`, screenRequestID, Name(pod))
	if err := v.sendFrame(0, 0, request); err != nil {
		return nil, err
	}
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case reply := <-v.screenReply:
		if reply.Error != nil {
			return nil, fmt.Errorf("hc read: %s", reply.Error.Message)
		}
		if reply.Result == nil {
			return nil, fmt.Errorf("hc read: missing result")
		}
		lines := strings.Split(reply.Result.Text, "\n")
		for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
			lines = lines[:len(lines)-1]
		}
		return lines, nil
	case <-v.done:
		return nil, fmt.Errorf("hc viewer closed")
	case <-timer.C:
		_ = v.cmd.Process.Kill()
		<-v.done
		return nil, fmt.Errorf("hc read: timed out")
	}
}
