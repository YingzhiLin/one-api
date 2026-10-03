#!/usr/bin/env bash
set -euo pipefail

one_api_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
one_api_pm2="${PM2_EXECUTABLE:-$(command -v pm2 || true)}"
if [[ -z "$one_api_pm2" ]]; then
  echo 'PM2 is not installed or not in PATH. Set PM2_EXECUTABLE to its executable.' >&2
  exit 1
fi

# Keep this project's process list separate from other PM2 deployments.
mkdir -p "$one_api_root/.runtime/pm2" "$one_api_root/logs"
chmod 700 "$one_api_root/.runtime" "$one_api_root/.runtime/pm2"
export PM2_HOME="$one_api_root/.runtime/pm2"
cd -- "$one_api_root"

# Avoid persisting unrelated shell credentials and stale app environment
# overrides in PM2's saved process list. Application configuration is in .env.
exec env -i HOME="$HOME" USER="$(id -un)" LOGNAME="$(id -un)" \
  PATH="$PATH" LANG="${LANG:-C.UTF-8}" PM2_HOME="$PM2_HOME" \
  "$one_api_pm2" "$@"
