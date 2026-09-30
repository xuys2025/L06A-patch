[CmdletBinding()]
param(
    [string]$SpeakerIP = '192.168.10.61'
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../lib/paths.ps1')
[void][System.Net.IPAddress]::Parse($SpeakerIP)
$tokenPath = Join-Path $AccessDirectory 'admin-token.txt'
$token = (Get-Content -LiteralPath $tokenPath -Raw).Trim()
if ($token.Length -lt 16) { throw '本地管理令牌无效。' }
$url = "http://${SpeakerIP}:8090/?token=$([uri]::EscapeDataString($token))"

try {
    $health = Invoke-WebRequest -Uri "http://${SpeakerIP}:8090/health" -TimeoutSec 4 -UseBasicParsing
    if ($health.Content.Trim() -ne 'ok') { throw '健康检查返回异常。' }
}
catch {
    throw "无法连接 $SpeakerIP`:8090。请先在路由器 DHCP 列表确认音箱 IP。原始错误：$($_.Exception.Message)"
}

Start-Process $url
Write-Host "已打开 L06A 管理页：$url"

