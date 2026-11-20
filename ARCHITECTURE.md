# Agent Sandbox — how it all fits together

Live walkthrough of our cluster (`kind-agent-sandbox`). Every name below is a
real object you can look at with k9s (`:sbt` `:swp` `:sbc` `:sbx`).

## The mental model (3 layers)

```text
┌────────────────────────────────────────────────────────────────────────────┐
│ LAYER 1 — MACHINERY (installed once, lives forever)                          │
│   4 CRDs + agent-sandbox-controller.                                        │
│   The controller is the ONLY thing that ever creates/deletes anything.      │
│   You never touch Pods or PVCs directly — you just declare intent in YAML.  │
├────────────────────────────────────────────────────────────────────────────┤
│ LAYER 2 — THE FACTORY (standing, applied once: demo/warmpool.yaml)          │
│   SandboxTemplate = the blueprint (what a sandbox looks like)               │
│   SandboxWarmPool = the inventory manager (always keep 2 warm on the shelf) │
├────────────────────────────────────────────────────────────────────────────┤
│ LAYER 3 — THE WORK UNIT (ephemeral, every cycle: demo/claim.yaml)           │
│   SandboxClaim → adopts one warm Sandbox → its Pod is where you work.       │
│   Delete the claim → sandbox+pod destroyed → factory restocks itself.      │
└────────────────────────────────────────────────────────────────────────────┘
```

## Big picture diagram

```text
                 ╔═══════════════════════════════════════════════════════╗
                 ║   agent-sandbox-controller  (the only worker)         ║
                 ║   namespace: agent-sandbox-system                    ║
                 ║   watches all 4 CRDs → creates/deletes/adopts stuff   ║
                 ╚═══════════════════╦═════════════════════════════════╝
                                     ▲  reconciles everything, forever
                                     │
  YOU ── apply demo/warmpool.yaml (ONCE)
    │
    ├─►  SandboxTemplate "agent-bash-template"      ← the BLUEPRINT
    │      image: nightshift-agent:0.1.0, runs `sleep infinity`, capped at 500m CPU and 256Mi
    │                     ▲
    │                     │  spec.sandboxTemplateRef
    │                     │
    └─►  SandboxWarmPool "agent-pool" (replicas: 2) ← the FACTORY
           │
           │ creates & owns            ┌────────────────────────────────┐
           ├──► agent-pool-b4tf2 ─────►│ Pod  Running, idle   (WARM #1) │
           └──► agent-pool-c9sqc ─────►│ Pod  Running, idle   (WARM #2) │
                                        └────────────────────────────────┘
```

## The claim cycle (your repeat loop)

```text
  YOU ── apply demo/claim.yaml (EVERY CYCLE)
    │
    └─►  SandboxClaim "my-agent"    (lifecycle.shutdownPolicy: Delete)
           │
           │  "give me a warm one"  →  INSTANT adoption, no cold start
           │                            (it was already Running seconds ago)
           ▼
        Sandbox "agent-pool-wklxl"          k9s: OWNER column = my-agent
           │  owned by SandboxClaim/my-agent
           │  claim status.sandbox.name = agent-pool-wklxl
           ▼
         Pod ──►   kubectl exec -it agent-pool-wklxl -- bash
                   ^^^^^^^^^^^^^^^^^ your work happens in here ^^^^^^^^^

  YOU ── kubectl delete -f demo/claim.yaml
    │
    └─►  sandbox + pod destroyed instantly (shutdownPolicy: Delete)
         factory restocks: builds a NEW warm standby → back to 2 on the shelf
    │
    └────────────── repeat from "apply demo/claim.yaml" ──────────────┘
```

## Who creates what

| Object                 | Who creates it          | Owned by (k9s OWNER col / shift-j) | Lifetime            |
|------------------------|-------------------------|-------------------------------------|---------------------|
| CRDs + controller      | release YAML (once)     | —                                   | life of cluster      |
| SandboxTemplate        | you, once               | you                                | standing             |
| SandboxWarmPool        | you, once               | you                                | standing             |
| warm Sandboxes (2)     | **controller**          | SandboxWarmPool "agent-pool"       | standing, auto-refill|
| claimed Sandbox         | **controller** (adopted)| SandboxClaim "my-agent"             | until claim deleted  |
| Pod                    | **controller**          | its Sandbox                        | same as its sandbox  |
| PVC (if template has one)| **controller**        | its Sandbox                        | same as its sandbox  |

## Files → objects

| File                             | Creates                          | Applied   |
|----------------------------------|---------------------------------------------|
| `sandbox-with-extensions.yaml` (upstream release, v1.0.11) | 4 CRDs + controller | once, forever |
| `demo/warmpool.yaml`             | Namespace + SandboxTemplate + SandboxWarmPool| once      |
| `demo/claim.yaml`                | SandboxClaim                     | per cycle |

## Same thing, mermaid version

```mermaid
flowchart TB
  subgraph standing["STANDING — demo/warmpool.yaml (apply once)"]
    T["SandboxTemplate<br/>agent-bash-template<br/>(blueprint)"]
    P["SandboxWarmPool agent-pool<br/>replicas: 2"]
    W1["warm sandbox<br/>agent-pool-b4tf2"]
    W2["warm sandbox<br/>agent-pool-c9sqc"]
  end
  subgraph cycle["PER CYCLE — demo/claim.yaml (apply/delete)"]
    C["SandboxClaim my-agent<br/>shutdownPolicy: Delete"]
    S["claimed sandbox<br/>owner: my-agent"]
    POD["Pod<br/>kubectl exec -- bash"]
  end
  CTRL["agent-sandbox-controller<br/>(does ALL the work)"]

  T -->|templateRef| P
  P -->|creates & owns| W1
  P -->|creates & owns| W2
  C ==adopts instantly==> S
  W1 -.becomes.- S
  S -->|creates| POD
  C -.delete claim.-> S
  S -.deleted.- C
  P ==restocks new warm==> W2
  CTRL -.watches & reconciles.-> T & P & C & S
```

## How to read it in k9s

| k9s view | What it answers |
|----------|-----------------|
| `:sbt`   | what blueprint exists |
| `:swp`   | factory health: `READY/DESIRED` (want 2/2) |
| `:sbc`   | which claim holds which sandbox (`SANDBOX` column) |
| `:sbx`   | every sandbox + who owns it (`OWNER` column: `my-agent` = claimed, `agent-pool` = warm) |
| `:po`    | the actual running containers |

Everything follows Kubernetes' declarative model: you never say "create a pod
for me" — you say "this is the state I want" (template/pool/claim YAML), and the
controller continuously makes reality match it.

## Multi-node and remote clusters

The controller does not change when nodes multiply. It runs once, watches all
nodes, and kube-scheduler places each sandbox Pod. The knobs below matter only
when nodes or claim volume grow.

- Placement: the template's `podTemplate.spec` is a full PodSpec. Use
  `nodeSelector`, `affinity`, or `topologySpreadConstraints` to keep warm
  sandboxes off one node. Use `runtimeClassName` to pin them to gVisor or Kata
  nodes.
- Claims stay node-agnostic. A claim takes any ready warm sandbox, on whatever
  node it was warmed.
- Slow nodes: a warm pod that reports its Pod IP late sends the claim to cold
  creation after `--sandbox-claim-warm-candidate-grace-period` (2s default). A
  warm sandbox that never goes Ready is replaced after the readiness grace
  period.
- Refill pacing: set `--sandbox-warm-pool-max-refill-rate` so a claim burst does
  not stampede the API server.
- Scale ceiling: one pool sustains 70 to 85 sandboxes per second. Above about
  30 claims per second or 500 replicas, split the capacity across several
  `SandboxWarmPool` resources and raise `--sandbox-warm-pool-concurrent-workers`
  to the pool count.
- Storage: a `ReadWriteOnce` PVC pins a sandbox to its node. Use network
  storage for stateful sandboxes, or no volume for throwaway ones.
- Node autoscaling: `--enable-warm-pool-eviction` (default true) marks warm pods
  evictable, so an autoscaler can reclaim nodes.
