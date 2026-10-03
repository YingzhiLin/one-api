#!/usr/bin/env bash
set -euo pipefail

one_api_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)"
one_api_pm2="${PM2_EXECUTABLE:-$(command -v pm2 || true)}"
if [[ -z "$one_api_pm2" ]]; then
  echo 'PM2 is not installed or not in PATH. Set PM2_EXECUTABLE to its executable.' >&2
  exit 1
fi

# Run as the deployment user; sudo is used only for the boot service.
"$one_api_root/scripts/pm2.sh" save
sudo env PATH="$PATH" PM2_HOME="$one_api_root/.runtime/pm2" \
  "$one_api_pm2" startup systemd -u "$(id -un)" --hp "$HOME" \
  --service-name pm2-one-api
