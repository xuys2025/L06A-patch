#!/bin/sh
set -eu

base=/data/assistant
config="$base/config.json"
backup="$base/config.json.before-control-fix"

[ -f /tmp/playerctl.sh ] || { echo "missing /tmp/playerctl.sh" >&2; exit 2; }
[ -f /tmp/volume.sh ] || { echo "missing /tmp/volume.sh" >&2; exit 2; }
[ -f "$config" ] || { echo "missing $config" >&2; exit 2; }

cp -p "$config" "$backup"
cp /tmp/playerctl.sh "$base/playerctl.sh"
cp /tmp/volume.sh "$base/volume.sh"
chmod 0755 "$base/playerctl.sh" "$base/volume.sh"

sed -i 's#"player_command": "[^"]*"#"player_command": "/data/assistant/playerctl.sh"#' "$config"
sed -i 's#"volume_command": "[^"]*"#"volume_command": "/data/assistant/volume.sh"#' "$config"

"$base/bin/assistant-agent" --check
pid=$(cat /var/run/assistant-agent.pid)
kill -HUP "$pid"
sleep 1

grep '"player_command"\|"volume_command"' "$config"
"$base/playerctl.sh" stop >/dev/null
echo "CONTROL_SCRIPTS_OK pid=$pid"
