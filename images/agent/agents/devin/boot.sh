# shellcheck shell=sh
# Inlined into the devin template's start command by k8s/warmpool.sh, after
# the logins are untarred. Keep lines within 70 columns.
# Let Devin act without asking, as agy does: the sandbox and the
# egress proxy are the guard rails. The image holds the version that
# was built and checked, so Devin does not update itself, and it sends
# its protobuf bodies plain so the proxy can swap its key in them. Its
# login, a placeholder, comes with the others from nightshift-auth.
d=$HOME/.config/devin
mkdir -p "$d"
jq -n '{permission_mode: "dangerous", auto_update: false,
  proxy: {gzip: false}}' > "$d/config.json"
