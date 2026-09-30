[CmdletBinding()]
param(
    [string]$SpeakerIP = '192.168.10.61',
    [string]$Binary = (Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'assistant-agent\dist\assistant-agent-linux-armv7'),
    [string]$PlayerScript = (Join-Path (Split-Path -Parent (Split-Path -Parent $PSScriptRoot)) 'firmware\overlay\usr\libexec\assistant\playerctl.sh')
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../lib/paths.ps1')
[void][System.Net.IPAddress]::Parse($SpeakerIP)
if (-not (Test-Path -LiteralPath $Binary -PathType Leaf)) { throw "程序不存在：$Binary" }
if (-not (Test-Path -LiteralPath $PlayerScript -PathType Leaf)) { throw "播放控制脚本不存在：$PlayerScript" }
$key = Join-Path $AccessDirectory 'id_rsa_l06a'
$knownHosts = Join-Path $AccessDirectory 'known_hosts'
$sha = (Get-FileHash -LiteralPath $Binary -Algorithm SHA256).Hash.ToLowerInvariant()
$playerSha = (Get-FileHash -LiteralPath $PlayerScript -Algorithm SHA256).Hash.ToLowerInvariant()
$lowSpaceUpdater = Join-Path $PSScriptRoot 'runtime/low_space_update.sh'
if (-not (Test-Path -LiteralPath $lowSpaceUpdater -PathType Leaf)) { throw "低空间更新脚本不存在：$lowSpaceUpdater" }
$sshOptions = @(
    '-i', $key,
    '-o', 'IdentitiesOnly=yes',
    '-o', 'HostKeyAlgorithms=+ssh-rsa',
    '-o', 'PubkeyAcceptedAlgorithms=+ssh-rsa',
    '-o', 'StrictHostKeyChecking=accept-new',
    '-o', "UserKnownHostsFile=$knownHosts",
    '-o', 'ConnectTimeout=8'
)
$scpOptions = @('-O') + $sshOptions
$destination = "root@${SpeakerIP}"

& scp.exe @scpOptions $Binary "${destination}:/tmp/assistant-agent.new"
if ($LASTEXITCODE -ne 0) { throw 'SCP 上传失败。' }
& scp.exe @scpOptions $PlayerScript "${destination}:/tmp/playerctl.sh.new"
if ($LASTEXITCODE -ne 0) { throw '播放控制脚本上传失败。' }
& scp.exe @scpOptions $lowSpaceUpdater "${destination}:/tmp/low_space_update.sh"
if ($LASTEXITCODE -ne 0) { throw '低空间更新脚本上传失败。' }

& ssh.exe @sshOptions $destination 'chmod 0755 /tmp/low_space_update.sh'
if ($LASTEXITCODE -ne 0) { throw '无法准备低空间更新脚本。' }
& ssh.exe @sshOptions $destination "/tmp/low_space_update.sh /tmp/assistant-agent.new $sha /tmp/playerctl.sh.new $playerSha"
if ($LASTEXITCODE -ne 0) { throw '网络更新失败；脚本已自动尝试恢复上一版本、PCM PNS、原厂 TTS 和播放控制脚本。' }

& ssh.exe @sshOptions $destination '/data/assistant/bin/assistant-agent --version; /data/assistant/bin/assistant-agent --check; /data/assistant/enable-native-speech.sh status; if [ -x /data/assistant/enable-native-tts-runtime.sh ]; then /data/assistant/enable-native-tts-runtime.sh status; fi; /data/assistant/playerctl.sh stop >/dev/null'
if ($LASTEXITCODE -ne 0) { throw '更新后状态检查失败。' }

Write-Host "网络更新完成，不需要重新刷机。binary=$sha player=$playerSha" -ForegroundColor Green
