[CmdletBinding()]
param(
    [string]$SpeakerIP = '192.168.10.61'
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../lib/paths.ps1')
[void][System.Net.IPAddress]::Parse($SpeakerIP)
$tokenPath = Join-Path $AccessDirectory 'admin-token.txt'
$token = (Get-Content -LiteralPath $tokenPath -Raw).Trim()
$headers = @{ 'X-Admin-Token' = $token }
$base = "http://${SpeakerIP}:8090"

function Read-SecretText {
    param([string]$Prompt)
    $secure = Read-Host $Prompt -AsSecureString
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($secure)
    try {
        return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer)
    }
    finally {
        [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer)
    }
}

$current = Invoke-RestMethod -Uri "$base/api/config" -Headers $headers -TimeoutSec 8
$config = $current.config

Write-Host ''
Write-Host '只需要输入以下 5 项。输入内容不会显示，也不会写入电脑文件。' -ForegroundColor Cyan
$asrKey = Read-SecretText '1/5 豆包 ASR API Key'
$llmBase = Read-Host '2/5 LLM Base URL（例如 https://服务商地址/v1）'
$llmModel = Read-Host '3/5 LLM Model（服务商给出的模型名称）'
$llmKey = Read-SecretText '4/5 LLM API Key'
$musicKey = Read-SecretText '5/5 MusicFree API Key'

if ([string]::IsNullOrWhiteSpace($asrKey) -or
    [string]::IsNullOrWhiteSpace($llmBase) -or
    [string]::IsNullOrWhiteSpace($llmModel) -or
    [string]::IsNullOrWhiteSpace($llmKey) -or
    [string]::IsNullOrWhiteSpace($musicKey)) {
    throw '有必填项为空，未保存任何更改。'
}

$config.llm_base_url = $llmBase.TrimEnd('/')
$config.llm_model = $llmModel.Trim()
$config.tts_engine = 'native'
$body = @{
    config = $config
    secrets = @{
        ASR_API_KEY = $asrKey
        LLM_API_KEY = $llmKey
        MUSIC_API_KEY = $musicKey
        TTS_API_KEY = ''
    }
} | ConvertTo-Json -Depth 12

$result = Invoke-RestMethod -Method Post -Uri "$base/api/config" -Headers $headers -ContentType 'application/json' -Body $body -TimeoutSec 15
if (-not $result.ok) { throw '音箱没有确认保存成功。' }
$status = Invoke-RestMethod -Uri "$base/api/status" -Headers $headers -TimeoutSec 8

Write-Host ''
Write-Host '配置已保存到音箱 /data，原厂 TTS 已保持启用。' -ForegroundColor Green
Write-Host "ASR=$($status.configured.asr)  LLM=$($status.configured.llm)  音乐=$($status.configured.music)"
Write-Host '现在可以在管理页点击“模拟唤醒”，或者直接说“小爱同学”测试。'

