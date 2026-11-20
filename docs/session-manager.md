# Sandbox terminal sessions

Nightshift uses [herdr-core](https://github.com/herdrdev/herdr-core) (`hc`)
inside each claimed sandbox to keep a shell alive when the dashboard navigates
away, exits, or loses its connection. The dashboard host needs `kubectl`, not
`hc`. Build and load the image as described in `images/agent/README.md` before
applying the warm pool manifest.

## How it works

For each claimed pod, Nightshift uses `kubectl exec` to start a detached Herdr
terminal running bash **inside the pod**. The terminal name is a short hash of
the pod name (Herdr limits names to 32 characters); a label records the full
pod name. The terminal is started with `--no-parent --rm`, without Herdr's
`--keep`: when bash exits, the terminal is removed rather than recovering into
a different shell. Warm pool pods do not get sessions. Claiming a pod prepares
its shell, and enter focuses it.

Herdr's holder owns the bash process independently of kubectl and the
dashboard. The embedded pane runs `hc read` in the pod and uses a framed
`kubectl exec -i ... -- hc connect` viewer to forward raw keystrokes and resize
events. Closing the dashboard closes the viewer, not the terminal. Press C-b
then q or click the sidebar to leave the embedded shell; `F` opens `hc attach`
fullscreen through kubectl, where Ctrl-\ detaches back to Nightshift.

Restarting Nightshift reuses live in-pod sessions. Deleting or restarting a
pod destroys its processes, including Herdr and bash; Herdr does not persist
the sandbox filesystem. Sessions created by older host-side versions are not
migrated or stopped.

Herdr runs as the sandbox's user and is not itself a security sandbox. Its
socket and saved terminal output should remain private; see Herdr's [security
documentation](https://github.com/herdrdev/herdr-core/blob/master/docs/security.md).
The integration uses Herdr's [preview generation-1 API](https://github.com/herdrdev/herdr-core/blob/master/schema/README.md).
