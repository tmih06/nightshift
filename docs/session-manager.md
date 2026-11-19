# Choosing the session manager for sandbox terminals

Research note, November 2026. Question: which session manager keeps bash
and running commands alive inside a sandbox when the dashboard navigates
away, stays low on resources, and makes terminals inside a sandbox easy
to manage. Decision: tmux, one session per sandbox pod.

## Requirements

1. Detach must not kill the work. Navigating the dashboard away from a
   sandbox, or quitting the dashboard, must leave the sandbox bash and
   whatever it runs alive.
2. Low resource usage. The manager runs on the same kind node family the
   demo targets, next to pods capped at 500m CPU and 256Mi memory.
3. Scriptable enough to hide. The dashboard drives it with commands, the
   user never types a tmux command unless they want to.

## The candidates

| Measure | screen 4.09 | tmux 3.4 | Zellij 0.42 |
|---|---|---|---|
| Binary size | 489 KB | 1.1 MB | 38.9 MB |
| Shared libraries | 9 | 15 | none, static |
| Session persistence | measured yes | measured yes | documented, not measured headless |
| Scriptable commands | limited, -X | 90 commands | a smaller CLI |
| Config | .screenrc | tmux command language | 433 lines of KDL |

Binary sizes and library counts are measured on one Linux machine by the
Urhoba comparison below; the session-persistence row is that author's
measurement of screen and tmux. Zellij refuses to run without a real TTY,
so its runtime behaviour was not measured there.

## Why tmux

The tmux manual page states the property requirement 1 needs: "A session
is a single collection of pseudo terminals under the management of tmux"
and "Each session is persistent and will survive accidental disconnection
... or intentional detaching". Sessions live in a server process, clients
attach and detach, and the work runs under the server, not the client.

That architecture is exactly the ownership the dashboard needs. The
dashboard client can exit while the tmux server keeps the kubectl exec
process, and the exec process keeps the pod's channel open. Requirement 1
is a detach, not a reconnect, so the feature does not depend on the
network at all.

Zellij matches the persistence feature on paper, but the measured binary
is 35 times larger than tmux and its interface, 14 modes and a Wasm
plugin runtime, is more than this dashboard hides behind two keys. GNU
screen is the smallest and would work, but its scripting is too thin for
the per-pod session naming and the lifecycle rules below. tmux is also
the one that is already installed almost everywhere this demo runs.

## The design that rides on it

One tmux session per sandbox pod, named `sbx-<pod>`. The session's single
pane runs the kubectl exec tunnel, `kubectl exec -it <pod> -- bash`.
Session names and pod names cannot collide: pod names are unique in the
namespace and the prefix keeps sessions out of the user's own namespace
of sessions.

The lifecycle is self-reaping. When a pod dies its kubectl exec exits,
the pane command finishes, and tmux destroys the session, so a session
can never outlive its sandbox. The dashboard never garbage collects
sessions, it only lists them.

The dashboard drives tmux with five commands, and the shell never
takes over the terminal.

- `new-session -d -s sbx-<pod> -x 160 -y 40 'kubectl exec -it <pod> --
  bash'` on the first attach, detached so the session starts without a
  client.
- `has-session -t` and one `list-sessions` to show the live-session
  dots in the left pane and the tmux row in the header.
- `resize-window -x W -y H` matches the session's window to the shell
  pane, so output wraps where the dashboard shows it.
- `capture-pane -p -J -t` every 200ms renders the session inside the
  dashboard's shell pane, bottom-aligned.
- `send-keys -t` carries the keystrokes typed while the shell pane has
  focus, so typing goes to bash without tmux owning the screen.
- `attach-session -t` remains only behind the F key, the escape hatch
  for full-screen programs like vim. Detach returns with C-b d.

Everything else, the pool refill, the claim teardown, the refresh, is
Kubernetes doing its own work.

## Verification

The e2e run on the kind cluster: claim through the dashboard's c key,
enter the claimed sandbox's bash, start a loop writing a counter to
/tmp/alive, detach with C-b d, quit the dashboard. The counter read 15
seconds of work at detach and 19 four seconds later, measured through
kubectl exec, so the command kept running after the dashboard exited.

The embedded shell was verified the same way on a real pty: enter
focused the shell pane, `echo E2E_EMBED_OK` typed through the pane ran
in the sandbox and its output rendered inside the dashboard frame, and
the alt screen was entered exactly once, at startup. Dragging the
splitter with SGR mouse sequences resized the left pane from 33 to 60
columns live.

## Sources

- tmux(1) manual page, [man7.org
  copy](https://man7.org/linux/man-pages/man1/tmux.1.html), session and
  client-server semantics quoted above.
- Urhoba. "tmux, Zellij or screen? All Three Measured", [measured
  comparison](https://www.urhoba.net/en/post/tmux-zellij-or-screen-measured),
  September 2026, binary sizes, dependencies, persistence tests.
- tmux project. [Getting Started
  wiki](https://github.com/tmux/tmux/wiki/Getting-Started), overview of
  the client-server model.
- Big Iron. "tmux vs Zellij vs screen in 2026", [feature
  comparison](https://www.bigiron.cc/guides/tmux-vs-zellij-vs-screen-in-2026),
  secondary source for Zellij's feature set.
