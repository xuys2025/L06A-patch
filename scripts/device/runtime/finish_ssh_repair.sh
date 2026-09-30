#!/bin/sh
set -eu

config=/data/assistant/config.json
secrets=/data/assistant/secrets.env
key_seed=/etc/assistant/authorized_keys.seed
key_data=/data/etc/dropbear/authorized_keys
key_target=/etc/dropbear/authorized_keys

cp -p "$config" "$config.before-ssh-repair-cleanup"
cp -p "$secrets" "$secrets.before-ssh-repair-cleanup"
if [ -x /data/assistant/native-tts.sh ]; then
    sed -i 's#"native_tts_command": "[^"]*"#"native_tts_command": "/data/assistant/native-tts.sh"#' "$config"
else
    sed -i 's#"native_tts_command": "[^"]*"#"native_tts_command": "/usr/libexec/assistant/native-tts.sh"#' "$config"
fi
[ -x /data/assistant/playerctl.sh ] && sed -i 's#"player_command": "[^"]*"#"player_command": "/data/assistant/playerctl.sh"#' "$config"
[ -x /data/assistant/volume.sh ] && sed -i 's#"volume_command": "[^"]*"#"volume_command": "/data/assistant/volume.sh"#' "$config"
sed -i 's#^TTS_API_KEY=.*#TTS_API_KEY=""#' "$secrets"

mkdir -p /data/etc/dropbear
cat "$key_seed" > "$key_data"
chmod 0600 "$key_data"
mount | grep -q 'on /etc/dropbear/authorized_keys ' || mount --bind "$key_data" "$key_target"

cat > /data/init.sh <<'EOF'
#!/bin/sh
mkdir -p /data/etc/dropbear
cat /etc/assistant/authorized_keys.seed > /data/etc/dropbear/authorized_keys
chmod 0600 /data/etc/dropbear/authorized_keys
mount | grep -q 'on /etc/dropbear/authorized_keys ' || mount --bind /data/etc/dropbear/authorized_keys /etc/dropbear/authorized_keys
[ -x /data/assistant/enable-native-speech.sh ] && /data/assistant/enable-native-speech.sh prepare >/tmp/native-speech-boot-prepare.log 2>&1 || true
(sleep 2; [ -x /data/assistant/enable-native-tts-runtime.sh ] && /data/assistant/enable-native-tts-runtime.sh start) >/tmp/native-tts-boot-start.log 2>&1 &
(sleep 12; [ -x /data/assistant/enable-native-speech.sh ] && /data/assistant/enable-native-speech.sh start) >/tmp/native-speech-boot-start.log 2>&1 &
(sleep 3; /etc/init.d/dropbear restart) >/tmp/dropbear-key-restart.log 2>&1 &
EOF
chmod 0700 /data/init.sh

mount | grep -q 'on /etc/passwd ' && umount /etc/passwd || true
rm -f /tmp/assistant-repair-ok /tmp/assistant-passwd-repair-ok /tmp/assistant-dropbear-key-repair-ok
kill -HUP "$(cat /var/run/assistant-agent.pid)"
echo SSH_REPAIR_CLEANUP_OK
