# A login surface for agent accounts

> **Superseded on 2026-11-27.** Nightshift no longer runs CLIProxyAPI. It
> signs accounts in with its own small service, nightshift-auth
> (`cmd/nightshift-auth`, see ARCHITECTURE.md), whose flows are ported from
> ompig-slim's PiG. CLIProxyAPI was a whole program run for one slice of it,
> needed a separate shell refresher and image to turn its credentials into
> sandbox logins, and had no Devin login. The note below is kept for the
> research behind the first choice.

Nightshift's sandboxes run the official Claude Code, Codex, and Antigravity
CLIs. Each needs a browser-approved OAuth login that nobody can script, and
the tokens it yields expire and rotate. This note records which open-source
projects already solve "sign an account in on a web page, keep it fresh", and
why Nightshift uses CLIProxyAPI for it. Researched 2026-11-25 from each
project's README, docs, and source on GitHub.

## Candidates

| Project | License | Runs on | Login surface | Fit |
| --- | --- | --- | --- | --- |
| [router-for-me/CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI) | MIT | One Go binary; file-backed credentials | Bundled management page plus a management API: `GET /v8/management/oauth/auth-url?provider=claude\|codex\|antigravity` returns the sign-in link; a pasted callback URL completes it. Refreshes tokens every 15 minutes. | **Chosen.** Smallest moving parts and exactly the three agents. |
| [Wei-Shaw/sub2api](https://github.com/Wei-Shaw/sub2api) | LGPL-3.0 | Go + PostgreSQL + Redis | Admin UI adds OAuth accounts | Heavier, built to resell shared subscriptions; its own README warns of terms-of-service risk. |
| [Wei-Shaw/claude-relay-service](https://github.com/Wei-Shaw/claude-relay-service) | MIT | Node + Redis | Admin UI adds Claude OAuth accounts | Claude-centred, and versions up to 1.1.248 had an admin authentication bypass. Superseded by sub2api. |
| [dvcrn/codex-oauth-proxy](https://github.com/dvcrn/codex-oauth-proxy), [dvcrn/antigravity-oauth-proxy](https://github.com/dvcrn/antigravity-oauth-proxy) | MIT / unlicensed | Small single-provider servers | CLI login only | One provider each; no management API. |
| [oauth2-proxy/oauth2-proxy](https://github.com/oauth2-proxy/oauth2-proxy) | MIT | Go reverse proxy | Signs *people* in to web apps | Not this problem: it never holds an agent account's tokens. |

## Why CLIProxyAPI

- **It is the login surface.** Its management page walks through each
  provider's sign-in. In a cluster the browser lands on a localhost address
  that will not load; pasting that full address into the page finishes the
  login, so no port has to be reachable.
- **It keeps tokens fresh and holds the refresh tokens.** Claude and OpenAI
  rotate refresh tokens on every use, so exactly one process may own them.
  CLIProxyAPI is that process; Nightshift's refresher holds none.
- **The management API is enough.** `GET /v8/management/credentials` lists
  accounts, `/credentials/download` returns one as flat JSON (`type`,
  `access_token`, `refresh_token`, `expired`, ...), and
  `POST /credentials/refresh` refreshes on demand. A `MANAGEMENT_PASSWORD`
  environment variable sets the key without writing it to a file.
- **It is not used as a gateway.** CLIProxyAPI can also re-serve a login as an
  OpenAI- or Claude-compatible API. Nightshift does not: no model traffic goes
  through it, and the sandboxes keep running the real CLIs with placeholders
  (see ARCHITECTURE.md). Its network policy admits only the refresher.

## Checked against the real thing

- v8.1.4 starts non-root in a pod, serves its page, and returns sign-in links
  for Claude (claude.ai), Codex (auth.openai.com), and Antigravity
  (accounts.google.com).
- An Antigravity credential uploaded through the management API was adopted
  and refreshed by CLIProxyAPI, read by the in-cluster refresher, published as
  a placeholder login, and the official `agy` answered a prompt from a sandbox.
- The Claude and Codex logins that `k8s/creds-providers.json` rebuilds from a
  CLIProxyAPI credential are accepted by the real `claude` and `codex`.
- The browser sign-in itself was not completed: it needs a person.

## Limits and risks

- **Terms of service.** Using a subscription login outside the vendor's own
  client can breach its terms, and vendors have enforced this. Nightshift's
  sandboxes do run the vendor's own CLI, but the sign-in is performed by
  CLIProxyAPI, which reuses the CLIs' public OAuth client identifiers. Read
  Anthropic's, OpenAI's, and Google's terms for your plan before relying on it.
- **Fallback chain across accounts.** The refresher publishes every enabled
  account of each provider in the order set in the Accounts tab
  (`POOL_<secret>`). The egress proxy sends requests to the first healthy
  account and transparently fails over down the chain on `429`/`401`/`403`/`529`
  with per-account cooldowns.
- **Claude's plan.** CLIProxyAPI does not record the subscription tier, so the
  rebuilt Claude login says `pro`. A Max plan may need that field corrected.
- **Single writer.** CLIProxyAPI runs as one replica on a ReadWriteOnce volume.
  Spreading it across replicas would let two processes rotate one refresh token.
- **Supply chain.** The management page is downloaded from GitHub by
  CLIProxyAPI itself (`disable-auto-update-panel` stops the updates).
