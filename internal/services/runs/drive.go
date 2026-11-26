package runs

import (
	"fmt"
	"time"

	v1 "github.com/tmih06/nightshift/api/v1"
	"github.com/tmih06/nightshift/internal/services/checkout"
	"github.com/tmih06/nightshift/internal/services/session"
)

const (
	cloneTimeout = 10 * time.Minute
	podTimeout   = 3 * time.Minute
	// grace is how long past its timeout an agent has to leave a result.
	grace = 2 * time.Minute
)

// task is what drive needs that the record does not keep.
type task struct {
	template string
	command  string
	request  v1.RunRequest
}

// drive takes a queued run to a working one, then watches it.
func (m *Manager) drive(e *entry, t task) {
	select {
	case m.setup <- struct{}{}:
	case <-e.ctx.Done():
		return
	}
	err := m.start(e, t)
	<-m.setup
	if err != nil {
		m.fail(e, err.Error())
		return
	}
	m.watch(e)
}

// start clones, makes the sandbox, and starts the agent in it. It stops early,
// without an error, once the run is deleted or the manager closes.
func (m *Manager) start(e *entry, t task) error {
	id := e.rec.Run.ID // never changes
	m.update(e, func(r *v1.Run) { r.State = v1.RunCloning })
	pod, image, err := m.ops.TemplatePod(t.template)
	if err != nil {
		return fmt.Errorf("read the template: %w", err)
	}
	if err := m.ops.Clone(id, image, cloneScript(id, t.request.Source), cloneTimeout, func(string) {}); err != nil {
		return fmt.Errorf("clone: %w", err)
	}
	dir := checkout.WorkspaceDir + "/" + t.request.Source.RepoName()
	sandbox, err := m.sandbox(e, t.template, pod, dir)
	if err != nil || sandbox == "" {
		return err
	}
	if err := m.ops.WaitPod(sandbox, podTimeout, func(string) {}); err != nil {
		return fmt.Errorf("start the sandbox: %w", err)
	}
	if stopped(e) {
		return nil
	}
	return m.launch(e, t, sandbox, dir)
}

// stopped is whether the run was deleted or the manager is closing.
func stopped(e *entry) bool { return e.ctx.Err() != nil }

// sandbox creates the run's sandbox, or none when the run is gone.
func (m *Manager) sandbox(e *entry, template string, pod map[string]any, dir string) (string, error) {
	e.step.Lock()
	defer e.step.Unlock()
	if stopped(e) {
		return "", nil
	}
	name, err := m.ops.CreateSandbox(template, pod, e.rec.Run.ID, dir)
	if err != nil {
		return "", fmt.Errorf("create the sandbox: %w", err)
	}
	m.update(e, func(r *v1.Run) { r.State, r.Sandbox = v1.RunStarting, name })
	return name, nil
}

// launch writes the prompt and script into the sandbox and types the script's
// name into its bash, so anyone can watch the agent work.
func (m *Manager) launch(e *entry, t task, sandbox, dir string) error {
	script := agentScript(t.command, dir, e.rec.Run.TimeoutSeconds)
	for name, content := range map[string]string{promptFile: t.request.Prompt, scriptFile: script} {
		if _, err := m.ops.Exec(sandbox, []byte(content), writeCommand(name)...); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	if err := m.ops.Ensure(sandbox); err != nil {
		return fmt.Errorf("start bash: %w", err)
	}
	line := "bash " + stateDir + "/" + scriptFile
	if err := m.ops.Send(sandbox, []session.Key{{Text: line}, {Name: "Enter"}}); err != nil {
		return fmt.Errorf("start the agent: %w", err)
	}
	now := time.Now().UTC()
	m.update(e, func(r *v1.Run) { r.State, r.StartedAt = v1.RunWorking, &now })
	return nil
}

// watch looks at a working run until the agent has exited, the sandbox is gone,
// or it is long past its timeout.
func (m *Manager) watch(e *entry) {
	m.mu.Lock()
	run := e.rec.Run
	m.mu.Unlock()
	started := run.CreatedAt
	if run.StartedAt != nil {
		started = *run.StartedAt
	}
	deadline := started.Add(time.Duration(run.TimeoutSeconds)*time.Second + grace)
	for m.pause(e.ctx, m.poll) {
		select {
		case m.look <- struct{}{}:
		case <-e.ctx.Done():
			return
		}
		ended := m.lookAt(e, run.ID, run.Sandbox)
		<-m.look
		if ended {
			return
		}
		if time.Now().After(deadline) {
			m.fail(e, "the agent left no result in time")
			return
		}
	}
}

// lookAt reads the agent's state once, and says whether the run has ended. A
// sandbox that cannot be reached is tried again, unless it no longer exists.
func (m *Manager) lookAt(e *entry, id, sandbox string) bool {
	var s status
	out, err := m.ops.Exec(sandbox, nil, "sh", "-c", statusScript)
	if err == nil {
		s, err = parseStatus(out)
	}
	if err != nil {
		if on, lookErr := m.ops.SandboxOn(id); lookErr != nil || on != "" {
			return false
		}
		s = status{gone: true}
	}
	switch {
	case s.gone:
		m.fail(e, "the sandbox is gone, and the run with it")
	case s.exited:
		m.update(e, func(r *v1.Run) { finish(r, s) })
	default:
		return false
	}
	return true
}

func finish(r *v1.Run, s status) {
	code := s.code
	r.State, r.ExitCode, r.Output = v1.RunDone, &code, s.output
	switch code {
	case 0:
	case 124:
		r.State, r.Error = v1.RunFailed, "the agent ran out of time"
	default:
		r.State, r.Error = v1.RunFailed, fmt.Sprintf("the agent exited with status %d", code)
	}
}
