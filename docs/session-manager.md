# Sandbox terminal sessions

Nightshift uses [herdr-core](https://github.com/herdrdev/herdr-core) (`hc`)
inside each claimed sandbox to keep a shell alive when the dashboard navigates
away, exits, or loses its connection. The dashboard host needs `kubectl`, not
`hc`. Ensure the published images are accessible as described in
`images/agent/README.md` before applying the warm pool manifest.

## How it works

For each claimed pod, Nightshift maintains one persistent framed
`kubectl exec -i <pod> -- hc connect` connection to the pod's Herdr core. Warm
pool pods never open a connection or start Herdr. Channel `0` speaks Herdr's
JSON-RPC 2.0 API (`terminal.list`, `terminal.get`, `terminal.run`,
`terminal.kill`, `terminal.attach`), and additional channels multiplex attached
terminal streams over that same connection.

Claiming a pod ensures at least one detached Herdr terminal running bash
**inside the pod** (`no_parent: true`, `rm: true`, without Herdr's `keep`: when
bash exits, the terminal is removed rather than recovering into a different
shell). Additional windows created from the dashboard—or background terminals
spawned inside the pod by an agent via `hc run --bg`—appear automatically in the
window tab bar right below the `● herdr live` status line.

Herdr's holder owns every shell process independently of kubectl and the
dashboard. Each attached window keeps its own local terminal emulator that
interprets the initial paint and subsequent output, preserving colors and other
text styles. It continues processing output while the tab is hidden, without
rendering the screen until the tab is visible again. Closing the dashboard
closes the `hc connect` tunnel, not the terminals inside the pod.

### Navigating windows

- **Mouse:** click any window tab to switch to it and focus the shell, click `+`
  to open a new bash window in the active window's current working directory,
  click `×` to close the active window, or middle-click any tab to close it.
- **Keyboard (`Ctrl-b` prefix while shell has focus):**
  - `C-b c` opens a new bash window in the active window's current working
    directory
  - `C-b n` / `C-b l` / `C-b →` and `C-b p` / `C-b h` / `C-b ←` cycle windows
  - `C-b 1`..`9` jumps directly to window 1..9
  - `C-b x` (or `C-b &`) closes the active window
  - `C-b F` (or `F` in navigation) opens `hc attach` fullscreen for the active
    window (`Ctrl-\` detaches back to Nightshift and reconnects the embedded
    viewer for a fresh paint)
  - `C-b C-b` sends a literal `Ctrl-b` to the shell
  - `C-b q` (or `C-b d`) returns focus to sandbox navigation

Restarting Nightshift reuses live in-pod sessions. Deleting or restarting a
pod destroys its processes, including Herdr and bash; Herdr does not persist
the sandbox filesystem. Sessions created by older host-side versions are not
migrated or stopped.

Herdr runs as the sandbox's user and is not itself a security sandbox. Its
socket and saved terminal output should remain private; see Herdr's [security
documentation](https://github.com/herdrdev/herdr-core/blob/master/docs/security.md).
The integration uses Herdr's [preview generation-1 API](https://github.com/herdrdev/herdr-core/blob/master/schema/README.md).
