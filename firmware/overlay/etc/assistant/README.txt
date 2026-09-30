L06A assistant runtime layout

/data/assistant/config.json       non-secret configuration
/data/assistant/secrets.env       ASR/LLM/music/TTS secrets, mode 0600
/data/assistant/admin.token       management page token
/data/assistant/bin/assistant-agent  optional network-installed override
/data/init.sh                     optional user boot hook

Web management: http://SPEAKER_IP:8090/?token=ADMIN_TOKEN
SSH: root, key seeded during image build; factory password remains available for recovery.
