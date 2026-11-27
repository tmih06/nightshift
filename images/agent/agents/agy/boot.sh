# shellcheck shell=sh
# Inlined into the agy template's start command by k8s/warmpool.sh, after
# the logins are untarred. Keep lines within 70 columns.
# Open agy straight at its prompt and let it act without asking:
# the sandbox and the egress proxy are the guard rails. agy trusts
# a folder only by its exact path, so list home and each workspace.
d=$HOME/.gemini/antigravity-cli
mkdir -p "$d/cache"
jq -n '{consumerOnboardingComplete: true,
  onboardingComplete: true}' > "$d/cache/onboarding.json"
{ echo "$HOME"; find "$HOME/workspace" -maxdepth 1 -type d; } |
  jq -Rn '{trustedWorkspaces: [inputs],
    toolPermission: "always-proceed",
    artifactReviewPolicy: "always-proceed",
    allowNonWorkspaceAccess: true}' > "$d/settings.json"
