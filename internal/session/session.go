// Package session manages Herdr terminals inside sandbox pods over a single
// framed `hc connect` connection per pod.
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
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/vt"

	"github.com/tmih06/nightshift/internal/cluster"
)

const (
	podLabel   = "nightshift-pod"
	rpcTimeout = 15 * time.Second
)

// Terminal describes one live Herdr terminal inside a sandbox pod.
type Terminal struct {
	Address   string
	Name      string
	Command   string
	Title     string
	Status    string
	CreatedMS uint64
}

// Label is the short name shown on a window tab. Explicit user/agent names
// win; auto-generated pod hashes fall back to the running foreground command.
func (t Terminal) Label() string {
	if t.Name != "" && !isGeneratedName(t.Name) {
		return t.Name
	}
	if t.Command != "" {
		return t.Command
	}
	if t.Name != "" {
		return t.Name
	}
	return t.Address
}

func isGeneratedName(name string) bool {
	if len(name) != 32 || !strings.HasPrefix(name, "sbx-") {
		return false
	}
	for _, r := range name[4:] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}

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

func missingHerdrErr(pod, text string) error {
	if strings.Contains(text, `exec: "hc": executable file not found`) {
		return fmt.Errorf("hc missing in %s; claim a new sandbox with the updated image", pod)
	}
	return nil
}

func hc(pod string, args ...string) ([]byte, error) {
	cmd := kubectl(pod, false, false, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if m := missingHerdrErr(pod, string(out)); m != nil {
			return nil, m
		}
		return nil, fmt.Errorf("hc %s in %s: %w: %s", args[0], pod, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// ---- per-pod framed hc connect client ------------------------------------

type rpcOutcome struct {
	raw    json.RawMessage
	parsed any
	err    error
}

type pendingCall struct {
	done     chan rpcOutcome
	onResult func(raw json.RawMessage) (any, error)
}

type podConn struct {
	pod    string
	cmd    *exec.Cmd
	in     io.WriteCloser
	reader *bufio.Reader
	stderr *bytes.Buffer

	writeMu sync.Mutex

	mu       sync.Mutex
	nextID   uint64
	closed   bool
	closeErr error
	done     chan struct{}
	pending  map[uint64]*pendingCall
	channels map[uint32]*viewer
	viewers  map[string]*viewer
}

var conns = struct {
	sync.Mutex
	byPod   map[string]*podConn
	dialing map[string]chan struct{}
}{
	byPod:   make(map[string]*podConn),
	dialing: make(map[string]chan struct{}),
}

func getPodConn(pod string) (*podConn, error) {
	for {
		conns.Lock()
		if c := conns.byPod[pod]; c != nil {
			c.mu.Lock()
			closed := c.closed
			c.mu.Unlock()
			if !closed {
				conns.Unlock()
				return c, nil
			}
			delete(conns.byPod, pod)
		}
		if wait := conns.dialing[pod]; wait != nil {
			conns.Unlock()
			<-wait
			continue
		}
		wait := make(chan struct{})
		conns.dialing[pod] = wait
		conns.Unlock()

		c, err := openPodConn(pod)

		conns.Lock()
		delete(conns.dialing, pod)
		if err == nil {
			conns.byPod[pod] = c
		}
		close(wait)
		conns.Unlock()
		return c, err
	}
}

func openPodConn(pod string) (*podConn, error) {
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
	fail := func(err error) (*podConn, error) {
		_ = in.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		if m := missingHerdrErr(pod, stderr.String()); m != nil {
			return nil, m
		}
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, fmt.Errorf("hc connect %s: %w: %s", pod, err, msg)
		}
		return nil, fmt.Errorf("hc connect %s: %w", pod, err)
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
	c := &podConn{
		pod:      pod,
		cmd:      cmd,
		in:       in,
		reader:   r,
		stderr:   &stderr,
		nextID:   2,
		done:     make(chan struct{}),
		pending:  make(map[uint64]*pendingCall),
		channels: make(map[uint32]*viewer),
		viewers:  make(map[string]*viewer),
	}
	go c.readLoop()
	return c, nil
}

func (c *podConn) writeFrame(channel uint32, kind byte, payload []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return fmt.Errorf("hc connection closed")
	}
	return writeFrame(c.in, channel, kind, payload)
}

func (c *podConn) call(method string, params any, dst any) error {
	out, err := c.callHook(method, params, nil)
	if err != nil {
		return err
	}
	if dst != nil && len(out.raw) > 0 {
		if err := json.Unmarshal(out.raw, dst); err != nil {
			return fmt.Errorf("hc %s decode: %w", method, err)
		}
	}
	return nil
}

func (c *podConn) callHook(method string, params any, onResult func(json.RawMessage) (any, error)) (rpcOutcome, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return rpcOutcome{}, fmt.Errorf("hc connection closed")
	}
	id := c.nextID
	c.nextID++
	pc := &pendingCall{done: make(chan rpcOutcome, 1), onResult: onResult}
	c.pending[id] = pc
	c.mu.Unlock()

	if params == nil {
		params = map[string]any{}
	}
	req, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	})
	if err != nil {
		c.removePending(id)
		return rpcOutcome{}, err
	}
	if err := c.writeFrame(0, 0, req); err != nil {
		c.removePending(id)
		return rpcOutcome{}, err
	}
	timer := time.NewTimer(rpcTimeout)
	defer timer.Stop()
	select {
	case out := <-pc.done:
		return out, out.err
	case <-c.done:
		c.removePending(id)
		c.mu.Lock()
		err := c.closeErr
		c.mu.Unlock()
		if err == nil {
			err = fmt.Errorf("hc connection closed")
		}
		return rpcOutcome{}, err
	case <-timer.C:
		c.removePending(id)
		return rpcOutcome{}, fmt.Errorf("hc %s timed out", method)
	}
}

func (c *podConn) removePending(id uint64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

func (c *podConn) readLoop() {
	var readErr error
	defer func() {
		c.mu.Lock()
		c.closed = true
		c.closeErr = readErr
		pending := c.pending
		c.pending = nil
		vs := make([]*viewer, 0, len(c.channels))
		for _, v := range c.channels {
			vs = append(vs, v)
		}
		c.channels = nil
		c.viewers = nil
		close(c.done)
		c.mu.Unlock()

		_ = c.in.Close()
		if c.cmd != nil {
			_ = c.cmd.Wait()
		}
		for _, pc := range pending {
			err := readErr
			if err == nil {
				err = fmt.Errorf("hc connection closed")
			}
			pc.done <- rpcOutcome{err: err}
		}
		for _, v := range vs {
			v.closeLocal()
		}
		conns.Lock()
		if conns.byPod[c.pod] == c {
			delete(conns.byPod, c.pod)
		}
		conns.Unlock()
	}()

	for {
		channel, kind, payload, err := readFrame(c.reader)
		if err != nil {
			readErr = err
			return
		}
		if channel == 0 {
			if kind == 0 {
				c.handleRPC(payload)
			}
			continue
		}
		c.mu.Lock()
		v := c.channels[channel]
		c.mu.Unlock()
		if v == nil {
			continue
		}
		switch kind {
		case 0: // DATA
			if len(payload) > 0 {
				v.applyOutput(payload)
				var credit [4]byte
				binary.LittleEndian.PutUint32(credit[:], uint32(len(payload)))
				if c.writeFrame(channel, 1, credit[:]) != nil {
					readErr = fmt.Errorf("hc credit write failed")
					return
				}
			}
		case 2: // CLOSE
			c.detachViewer(v, false)
		}
	}
}

type rpcError struct {
	Code    int
	Kind    string
	Message string
}

func (e *rpcError) Error() string { return e.Message }

func isRPCKind(err error, kind string) bool {
	re, ok := err.(*rpcError)
	return ok && re.Kind == kind
}

func (c *podConn) handleRPC(payload []byte) {
	var env struct {
		ID     *uint64         `json:"id"`
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
			Data    *struct {
				Kind string `json:"kind"`
			} `json:"data"`
		} `json:"error"`
	}
	if err := json.Unmarshal(payload, &env); err != nil || env.ID == nil {
		return
	}
	c.mu.Lock()
	pc := c.pending[*env.ID]
	delete(c.pending, *env.ID)
	c.mu.Unlock()
	if pc == nil {
		return
	}
	if env.Error != nil {
		re := &rpcError{Code: env.Error.Code, Message: env.Error.Message}
		if env.Error.Data != nil {
			re.Kind = env.Error.Data.Kind
		}
		pc.done <- rpcOutcome{err: re}
		return
	}
	if pc.onResult != nil {
		parsed, err := pc.onResult(env.Result)
		pc.done <- rpcOutcome{raw: env.Result, parsed: parsed, err: err}
		return
	}
	pc.done <- rpcOutcome{raw: env.Result}
}

func (c *podConn) detachViewer(v *viewer, notifyCore bool) {
	c.mu.Lock()
	removed := false
	if c.channels != nil && c.channels[v.channel] == v {
		delete(c.channels, v.channel)
		removed = true
	}
	if c.viewers != nil {
		for k, cur := range c.viewers {
			if cur == v {
				delete(c.viewers, k)
			}
		}
	}
	c.mu.Unlock()
	if notifyCore && removed {
		_ = c.writeFrame(v.channel, 2, nil)
	}
	v.closeLocal()
}

func (c *podConn) close() {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	_ = c.in.Close()
}

// ClosePod tears down the cached hc connect tunnel for pod, without stopping
// any Herdr terminals running inside it.
func ClosePod(pod string) {
	conns.Lock()
	c := conns.byPod[pod]
	if c != nil {
		delete(conns.byPod, pod)
	}
	conns.Unlock()
	if c != nil {
		c.close()
	}
}

// ---- terminal lifecycle API ----------------------------------------------

type wireTerminal struct {
	Address    string            `json:"address"`
	Name       *string           `json:"name"`
	Argv       []string          `json:"argv"`
	Title      string            `json:"title"`
	State      string            `json:"state"`
	Labels     map[string]string `json:"labels"`
	CreatedMS  uint64            `json:"created_ms"`
	Foreground *struct {
		Name string `json:"name"`
	} `json:"foreground"`
	Status *struct {
		State string `json:"state"`
	} `json:"status"`
}

func (w wireTerminal) toTerminal() Terminal {
	t := Terminal{
		Address:   w.Address,
		Title:     w.Title,
		CreatedMS: w.CreatedMS,
	}
	if w.Name != nil {
		t.Name = *w.Name
	}
	if w.Foreground != nil && w.Foreground.Name != "" {
		t.Command = w.Foreground.Name
	} else if len(w.Argv) > 0 && w.Argv[0] != "" {
		t.Command = path.Base(w.Argv[0])
	}
	if w.Status != nil {
		t.Status = w.Status.State
	}
	return t
}

// List returns every live Herdr terminal inside pod, ordered by creation time.
func List(pod string) ([]Terminal, error) {
	c, err := getPodConn(pod)
	if err != nil {
		return nil, err
	}
	var res struct {
		Terminals []wireTerminal `json:"terminals"`
	}
	if err := c.call("terminal.list", map[string]any{}, &res); err != nil {
		return nil, err
	}
	out := make([]Terminal, 0, len(res.Terminals))
	for _, wt := range res.Terminals {
		if wt.State != "live" {
			continue
		}
		out = append(out, wt.toTerminal())
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedMS != out[j].CreatedMS {
			return out[i].CreatedMS < out[j].CreatedMS
		}
		return out[i].Address < out[j].Address
	})
	return out, nil
}

// Create starts a new detached bash terminal inside pod. Pass an empty name
// to let Herdr assign a unique address (t1, t2, ...).
func Create(pod, name string) (Terminal, error) {
	c, err := getPodConn(pod)
	if err != nil {
		return Terminal{}, err
	}
	params := map[string]any{
		"argv":      []string{"bash"},
		"no_parent": true,
		"rm":        true,
		"labels":    map[string]string{podLabel: pod},
	}
	if name != "" {
		params["name"] = name
	}
	var wt wireTerminal
	if err := c.call("terminal.run", params, &wt); err != nil {
		return Terminal{}, err
	}
	return wt.toTerminal(), nil
}

// Kill terminates a single Herdr terminal inside pod. Terminals started with
// rm: true are removed as soon as they exit, causing herdr-core's terminal.kill
// to return a not_found ("<term> was removed") error after killing them; treat
// not_found as success since the terminal is gone.
func Kill(pod, term string) error {
	if term == "" {
		term = Name(pod)
	}
	DisconnectViewer(pod, term)
	c, err := getPodConn(pod)
	if err != nil {
		return err
	}
	if err := c.call("terminal.kill", map[string]any{"terminal": term}, nil); err != nil && !isRPCKind(err, "not_found") {
		return err
	}
	return nil
}

// Ensure guarantees pod has at least one live Herdr bash terminal.
func Ensure(pod string) error {
	terms, err := List(pod)
	if err == nil && len(terms) > 0 {
		return nil
	}
	if _, err := Create(pod, Name(pod)); err != nil {
		// Another request may have created it concurrently.
		if terms, lerr := List(pod); lerr == nil && len(terms) > 0 {
			return nil
		}
		return err
	}
	return nil
}

func Has(pod string) bool {
	terms, err := List(pod)
	return err == nil && len(terms) > 0
}

func Attach(pod, term string) *exec.Cmd {
	if term == "" {
		term = Name(pod)
	}
	return kubectl(pod, true, true, "attach", term)
}

// DisconnectViewer detaches one embedded terminal stream (or all streams for
// pod when term is empty) so the next embedded view gets a fresh paint. It
// does not stop the shell, which is owned by Herdr's holder.
func DisconnectViewer(pod, term string) {
	conns.Lock()
	c := conns.byPod[pod]
	conns.Unlock()
	if c == nil {
		return
	}
	if term == "" {
		c.mu.Lock()
		vs := make([]*viewer, 0, len(c.channels))
		for _, v := range c.channels {
			vs = append(vs, v)
		}
		c.mu.Unlock()
		for _, v := range vs {
			c.detachViewer(v, true)
		}
		return
	}
	c.mu.Lock()
	v := c.viewers[term]
	c.mu.Unlock()
	if v != nil {
		c.detachViewer(v, true)
	}
}

// ---- viewer (one attached channel on a podConn) --------------------------

type Key struct {
	Text string
	Name string
}

// A viewer provides raw input, resize and a local screen for one terminal
// attached over its pod's shared hc connect stream.
type viewer struct {
	conn    *podConn
	term    string
	channel uint32
	in      io.WriteCloser // standalone test writer when conn == nil

	mu         sync.Mutex
	closed     bool
	dirty      chan struct{}
	done       chan struct{}
	screenMu   sync.Mutex
	screen     *vt.Emulator
	screenDone chan struct{}
}

func resolveTerm(pod, term string) string {
	if term == "" {
		return Name(pod)
	}
	return term
}

func getViewer(pod, term string, w, h int) (*viewer, error) {
	term = resolveTerm(pod, term)
	c, err := getPodConn(pod)
	if err != nil {
		return nil, err
	}
	return c.getViewer(term, w, h)
}

func (c *podConn) getViewer(term string, w, h int) (*viewer, error) {
	c.mu.Lock()
	if v := c.viewers[term]; v != nil {
		v.mu.Lock()
		closed := v.closed
		v.mu.Unlock()
		if !closed {
			c.mu.Unlock()
			return v, nil
		}
		delete(c.viewers, term)
	}
	c.mu.Unlock()

	// Register the viewer synchronously inside readLoop as soon as the
	// terminal.attach response arrives, before readLoop reads the first paint
	// frame on the new channel.
	onResult := func(raw json.RawMessage) (any, error) {
		var resp struct {
			Channel uint32 `json:"channel"`
		}
		if err := json.Unmarshal(raw, &resp); err != nil {
			return nil, err
		}
		v := &viewer{
			conn:    c,
			term:    term,
			channel: resp.Channel,
			dirty:   make(chan struct{}, 1),
			done:    make(chan struct{}),
			screen:  vt.NewEmulator(w, h),
		}
		// Herdr owns scrollback; the embedded pane only renders the live screen.
		v.screen.SetScrollbackSize(1)
		v.drainScreenReplies()
		c.mu.Lock()
		if !c.closed {
			c.channels[resp.Channel] = v
			c.viewers[term] = v
		}
		c.mu.Unlock()
		return v, nil
	}
	out, err := c.callHook("terminal.attach", map[string]any{
		"terminal": term,
		"cols":     w,
		"rows":     h,
	}, onResult)
	if err != nil {
		return nil, fmt.Errorf("hc attach %s: %w", term, err)
	}
	return out.parsed.(*viewer), nil
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
	closed := v.closed
	v.mu.Unlock()
	if closed {
		return fmt.Errorf("hc viewer closed")
	}
	if v.conn != nil {
		return v.conn.writeFrame(v.channel, kind, payload)
	}
	return writeFrame(v.in, v.channel, kind, payload)
}

func (v *viewer) closeLocal() {
	v.mu.Lock()
	if v.closed {
		v.mu.Unlock()
		return
	}
	v.closed = true
	if v.done != nil {
		close(v.done)
	}
	v.mu.Unlock()
	v.closeScreen()
}

// Herdr answers terminal queries itself. Drain local emulator replies so its
// unbuffered input pipe cannot block, then stop the reader before Close.
func (v *viewer) drainScreenReplies() {
	v.screenDone = make(chan struct{})
	go func() {
		_, _ = io.Copy(io.Discard, v.screen)
		close(v.screenDone)
	}()
}

func (v *viewer) closeScreen() {
	if v.screen == nil {
		return
	}
	v.screenMu.Lock()
	defer v.screenMu.Unlock()
	_ = v.screen.InputPipe().(io.Closer).Close()
	if v.screenDone != nil {
		<-v.screenDone
	}
	_ = v.screen.Close()
}

func (v *viewer) applyOutput(payload []byte) {
	v.screenMu.Lock()
	_, _ = v.screen.Write(payload)
	v.screenMu.Unlock()
	v.markDirty()
}

func (v *viewer) markDirty() {
	select {
	case v.dirty <- struct{}{}:
	default:
	}
}

func (v *viewer) resize(w, h int) error {
	v.screenMu.Lock()
	// A redundant emulator resize resets scroll margins and tab stops.
	if v.screen.Width() != w || v.screen.Height() != h {
		v.screen.Resize(w, h)
	}
	v.screenMu.Unlock()
	var size [4]byte
	binary.LittleEndian.PutUint16(size[:], uint16(w))
	binary.LittleEndian.PutUint16(size[2:], uint16(h))
	return v.send(3, size[:])
}

func SendBatch(pod, term string, keys []Key) error {
	v, err := getViewer(pod, term, 160, 40)
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

// LiveTerminals checks claimed, running pods and returns each pod's live
// Herdr terminals; warm pods must not start Herdr. Stale connections to pods
// no longer in the fleet are closed.
func LiveTerminals(boxes []cluster.Sandbox) map[string][]Terminal {
	active := make(map[string]bool, len(boxes))
	for _, box := range boxes {
		if box.Claimed() && box.Phase == "Running" {
			active[box.Name] = true
		}
	}
	conns.Lock()
	var stale []*podConn
	for pod, c := range conns.byPod {
		if !active[pod] {
			delete(conns.byPod, pod)
			stale = append(stale, c)
		}
	}
	conns.Unlock()
	for _, c := range stale {
		c.close()
	}

	result := map[string][]Terminal{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, box := range boxes {
		if !box.Claimed() || box.Phase != "Running" {
			continue
		}
		wg.Add(1)
		go func(pod string) {
			defer wg.Done()
			if terms, err := List(pod); err == nil && len(terms) > 0 {
				mu.Lock()
				result[pod] = terms
				mu.Unlock()
			}
		}(box.Name)
	}
	wg.Wait()
	return result
}

// Live checks claimed, running pods; warm pods must not start Herdr.
func Live(boxes []cluster.Sandbox) map[string]bool {
	terms := LiveTerminals(boxes)
	live := make(map[string]bool, len(terms))
	for pod := range terms {
		live[pod] = true
	}
	return live
}
