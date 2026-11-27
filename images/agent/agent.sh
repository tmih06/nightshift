#!/usr/bin/env bash
# agent.sh — the one place an agent image learns how to install its agent.
#
# Each agent is a directory under agents/ named after it (the image, pool, and
# template are named the same). Its agent.env sets COMMAND, the CLI it puts on
# PATH, and either NPM_PACKAGE for an npm install or its own install.sh and
# latest.sh. Adding an agent is adding a directory; nothing here changes.
#
# Usage: agent.sh list                     agents as a JSON array
#        agent.sh command AGENT            the CLI the agent installs
#        agent.sh latest  AGENT            its latest stable version
#        agent.sh install AGENT VERSION    install it (in the image build)
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
semver='^[0-9]+\.[0-9]+\.[0-9]+$'

die() { echo "agent.sh: $*" >&2; exit 1; }

load() {
  dir=$here/agents/$1
  [ -f "$dir/agent.env" ] || die "unsupported agent: $1"
  COMMAND="" NPM_PACKAGE=""
  # shellcheck source=/dev/null
  . "$dir/agent.env"
  [ -n "$COMMAND" ] || die "$1: agent.env sets no COMMAND"
}

stable() {
  echo "$1" | grep -Eq "$semver" || die "refusing non-stable version: '$1'"
  echo "$1"
}

case "${1:-}" in
  list)
    find "$here/agents" -mindepth 2 -maxdepth 2 -name agent.env -printf '%h\n' |
      xargs -n1 basename | sort | jq -Rcn '[inputs]'
    ;;
  command)
    load "${2:?agent}"
    echo "$COMMAND"
    ;;
  latest)
    load "${2:?agent}"
    if [ -n "$NPM_PACKAGE" ]; then
      v=$(curl --fail --silent --show-error --retry 3 \
        "https://registry.npmjs.org/$NPM_PACKAGE/latest" | jq -er .version)
    else
      v=$(bash "$dir/latest.sh")
    fi
    stable "$v"
    ;;
  install)
    load "${2:?agent}"
    VERSION=$(stable "${3:?version}")
    export VERSION
    if [ -n "$NPM_PACKAGE" ]; then
      npm install --global "$NPM_PACKAGE@$VERSION"
      npm cache clean --force
    else
      bash "$dir/install.sh"
    fi
    "$COMMAND" --version | grep -F "$VERSION"
    echo "$2 $VERSION" > /etc/nightshift-agent
    ;;
  *)
    die "usage: agent.sh list | command AGENT | latest AGENT | install AGENT VERSION"
    ;;
esac
