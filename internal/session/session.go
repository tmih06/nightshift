// Package session manages Herdr terminals inside sandbox pods.
package session

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"

	"github.com/tmih06/nightshift/internal/cluster"
)

const podLabel = "nightshift-pod"

// Herdr names are limited to 32 characters, while pod names may be longer.
func Name(pod string) string {
	sum := sha256.Sum256([]byte(pod))
	return fmt.Sprintf("sbx-%x", sum[:14])
}

func kubectl(pod string, tty, stdin bool, args ...string) *exec.Cmd {
	command := []string{"--context", cluster.KubeContext, "-n", cluster.Namespace, "exec"}
	if stdin {
		command = append(command, "-i")
	}
	if tty {
		command = append(command, "-t")
	}
	command = append(command, pod, "--", "hc")
	return exec.Command("kubectl", append(command, args...)...)
}

func hc(pod string, args ...string) ([]byte, error) {
	cmd := kubectl(pod, false, false, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), `exec: "hc": executable file not found`) {
			return nil, fmt.Errorf("hc missing in %s; claim a new sandbox with the updated image", pod)
		}
		return nil, fmt.Errorf("hc %s in %s: %w: %s", args[0], pod, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func Ensure(pod string) error {
	if Has(pod) {
		return nil
	}
	_, err := hc(pod, "run", "--bg", "--no-parent", "--rm", "--name", Name(pod), "--label", podLabel+"="+pod, "--", "bash")
	if err != nil && Has(pod) { // Another request may have created it concurrently.
		return nil
	}
	return err
}

func Has(pod string) bool {
	out, err := hc(pod, "get", "--json", Name(pod))
	if err != nil {
		return false
	}
	var terminal struct {
		State  string            `json:"state"`
		Labels map[string]string `json:"labels"`
	}
	return json.Unmarshal(out, &terminal) == nil && terminal.State == "live" && terminal.Labels[podLabel] == pod
}

func Attach(pod string) *exec.Cmd {
	return kubectl(pod, true, true, "attach", Name(pod))
}

func Capture(pod string) ([]string, error) {
	out, err := hc(pod, "read", "--json", Name(pod))
	if err != nil {
		return nil, err
	}
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("hc read: %w", err)
	}
	lines := strings.Split(result.Text, "\n")
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return lines, nil
}

type Key struct {
	Text string
	Name string
}

// A viewer provides raw input and resize. hc keys/send are intended for agent
// messages and may wait for a quiet period or append Enter, so they cannot
// carry interactive keystrokes. The holder, not this viewer, owns the shell.
type viewer struct {
	mu      sync.Mutex
	in      io.WriteCloser
	cmd     *exec.Cmd
	reader  *bufio.Reader
	channel uint32
	closed  bool
}

var viewers = struct {
	sync.Mutex
	byPod map[string]*viewer
}{byPod: make(map[string]*viewer)}

func getViewer(pod string, w, h int) (*viewer, error) {
	viewers.Lock()
	defer viewers.Unlock()
	if v := viewers.byPod[pod]; v != nil {
		v.mu.Lock()
		closed := v.closed
		v.mu.Unlock()
		if !closed {
			return v, nil
		}
		delete(viewers.byPod, pod)
	}
	v, err := openViewer(pod, w, h)
	if err != nil {
		return nil, err
	}
	viewers.byPod[pod] = v
	go v.read(pod)
	return v, nil
}

func openViewer(pod string, w, h int) (*viewer, error) {
	cmd := kubectl(pod, false, true, "connect")
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("hc connect: %w", err)
	}
	fail := func(err error) (*viewer, error) {
		_ = in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return nil, fmt.Errorf("hc attach %s: %w: %s", Name(pod), err, strings.TrimSpace(stderr.String()))
	}
	r := bufio.NewReader(out)
	if _, err := io.WriteString(in, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"hello\",\"params\":{\"framed\":true}}\n"); err != nil {
		return fail(err)
	}
	line, err := r.ReadBytes('\n')
	if err != nil {
		return fail(err)
	}
	var reply struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(line, &reply); err != nil {
		return fail(err)
	}
	if reply.Error != nil {
		return fail(fmt.Errorf("%s", reply.Error.Message))
	}
	request, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 2, "method": "terminal.attach", "params": map[string]any{"terminal": Name(pod), "cols": w, "rows": h}})
	if err := writeFrame(in, 0, 0, request); err != nil {
		return fail(err)
	}
	for {
		channel, kind, payload, err := readFrame(r)
		if err != nil {
			return fail(err)
		}
		if channel != 0 || kind != 0 {
			continue
		}
		var response struct {
			Result struct {
				Channel uint32 `json:"channel"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := json.Unmarshal(payload, &response); err != nil {
			return fail(err)
		}
		if response.Error != nil {
			return fail(fmt.Errorf("%s", response.Error.Message))
		}
		return &viewer{in: in, cmd: cmd, reader: r, channel: response.Result.Channel}, nil
	}
}

func writeFrame(w io.Writer, channel uint32, kind byte, payload []byte) error {
	header := make([]byte, 9)
	binary.LittleEndian.PutUint32(header, channel)
	header[4] = kind
	binary.LittleEndian.PutUint32(header[5:], uint32(len(payload)))
	frame := append(header, payload...)
	n, err := w.Write(frame)
	if err == nil && n != len(frame) {
		return io.ErrShortWrite
	}
	return err
}

func readFrame(r io.Reader) (uint32, byte, []byte, error) {
	var header [9]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return 0, 0, nil, err
	}
	n := binary.LittleEndian.Uint32(header[5:])
	if n > 16<<20 {
		return 0, 0, nil, fmt.Errorf("hc frame too large: %d", n)
	}
	payload := make([]byte, n)
	_, err := io.ReadFull(r, payload)
	return binary.LittleEndian.Uint32(header[:]), header[4], payload, err
}

func (v *viewer) send(kind byte, payload []byte) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.closed {
		return fmt.Errorf("hc viewer closed")
	}
	return writeFrame(v.in, v.channel, kind, payload)
}

func (v *viewer) read(pod string) {
	defer func() {
		v.mu.Lock()
		v.closed = true
		_ = v.in.Close()
		v.mu.Unlock()
		_ = v.cmd.Wait()
		viewers.Lock()
		if viewers.byPod[pod] == v {
			delete(viewers.byPod, pod)
		}
		viewers.Unlock()
	}()
	for {
		channel, kind, payload, err := readFrame(v.reader)
		if err != nil || (channel == v.channel && kind == 2) {
			return
		}
		if channel == v.channel && kind == 0 && len(payload) > 0 {
			var credit [4]byte
			binary.LittleEndian.PutUint32(credit[:], uint32(len(payload)))
			if v.send(1, credit[:]) != nil {
				return
			}
		}
	}
}

func Resize(pod string, w, h int) error {
	if w < 1 || h < 1 || w > 65535 || h > 65535 {
		return fmt.Errorf("invalid terminal size %dx%d", w, h)
	}
	v, err := getViewer(pod, w, h)
	if err != nil {
		return err
	}
	var size [4]byte
	binary.LittleEndian.PutUint16(size[:], uint16(w))
	binary.LittleEndian.PutUint16(size[2:], uint16(h))
	return v.send(3, size[:])
}

func SendBatch(pod string, keys []Key) error {
	v, err := getViewer(pod, 160, 40)
	if err != nil {
		return err
	}
	var data strings.Builder
	for _, key := range keys {
		if key.Text != "" {
			data.WriteString(key.Text)
			continue
		}
		if s, ok := keyBytes(key.Name); ok {
			data.WriteString(s)
		}
	}
	if data.Len() == 0 {
		return nil
	}
	return v.send(0, []byte(data.String()))
}

func keyBytes(name string) (string, bool) {
	keys := map[string]string{"Enter": "\r", "Escape": "\x1b", "Up": "\x1b[A", "Down": "\x1b[B", "Right": "\x1b[C", "Left": "\x1b[D", "BSpace": "\x7f", "DC": "\x1b[3~", "Home": "\x1b[H", "End": "\x1b[F", "PageUp": "\x1b[5~", "PageDown": "\x1b[6~", "Tab": "\t", "BTab": "\x1b[Z"}
	if s, ok := keys[name]; ok {
		return s, true
	}
	if len(name) == 3 && strings.HasPrefix(name, "C-") && name[2] >= 'a' && name[2] <= 'z' {
		return string(rune(name[2] - 'a' + 1)), true
	}
	return "", false
}

// Live checks claimed, running pods; warm pods must not start Herdr.
func Live(boxes []cluster.Sandbox) map[string]bool {
	live := map[string]bool{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, box := range boxes {
		if box.OwnerKind != "SandboxClaim" || box.Phase != "Running" {
			continue
		}
		wg.Add(1)
		go func(pod string) {
			defer wg.Done()
			if Has(pod) {
				mu.Lock()
				live[pod] = true
				mu.Unlock()
			}
		}(box.Name)
	}
	wg.Wait()
	return live
}
