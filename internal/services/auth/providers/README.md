# Sign-in flows

Each file here is one coding agent's sign-in for nightshift-auth: a type
implementing `Provider` (`provider.go`), listed in `All`.

## Adding an agent

1. Write `<agent>.go` with its `Info` (id, name, egress secret, warm pool,
   where its CLI keeps its login, and the API hosts that get the token),
   `Begin` and `Finish` (OAuth with PKCE; `Finish` takes what the person
   pastes: the code the provider shows, or the address the browser ends on),
   `Refresh` (`ErrNoRefresh` when the login cannot be renewed),
   `SandboxLogin` (built from a named list of fields, the placeholder for the
   token, nothing that names the account), and `Companions` (other
   account-bound values the proxy swaps, by secret name).
2. Add it to `All`.
3. Bind its secret in `k8s/egress.yaml` (`file`, `label_file`, `pool_file`
   under `/etc/egress-secrets/`) and swap it on its hosts in a `mitm` rule.
4. Give it an image directory under `images/agent/agents` and an entry in
   `internal/services/agent` if it is a new CLI.

`go test ./internal/services/auth/...` fails until the proxy binds and swaps
the secret (`TestEveryAgentIsSwappedByTheProxy`) and the sandbox login carries
no account (`TestSandboxLoginsNameNoAccount`).

## Where the flows come from

They are ported from [PiG](https://github.com/MichaelKinsy/PiG) (MIT,
Copyright Michael Kinsy) as vendored in
[tmih06/ompig-slim](https://github.com/tmih06/ompig-slim) at commit
`5d7a29d`, which carries the Devin flow and the Antigravity client fix:

| Here | There (`vendor/github.com/MichaelKinsy/PiG/ai/`) |
| --- | --- |
| `claude.go` | `oauth_anthropic.go` (copy-code login) |
| `codex.go` | `oauth_openai_codex.go` (browser login) |
| `antigravity.go` | `google_cloud_code_auth.go` (Antigravity's client) |
| `devin.go` | `oauth_devin.go`, `devin_proto.go` (session token prefix) |
| `oauth.go` | PKCE and paste parsing from `oauth_*.go` |

Left out: the local callback servers (in a cluster the browser's localhost
address never loads, so the paste is the only way), device-code logins,
Antigravity's Cloud Code project binding (agy binds its own), and Devin's
identity lookup over Connect RPC (the session token's claims name the
account). The Claude profile lookup, which fills in the plan a sandbox login
names, is new here.
