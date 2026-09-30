[CmdletBinding()]
param(
    [switch]$PreflightOnly,
    [ValidateRange(10, 600)]
    [int]$CaptureTimeoutSec = 120,
    [ValidateRange(0, 2000)]
    [int]$PollIntervalMilliseconds = 100
)

$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot '../lib/paths.ps1')
$workspace = $ProjectRoot
$tool = Join-Path $LocalDirectory 'tools\Amlogic_Flash_Tool_v6.0.0\bin\update.exe'
$image = Join-Path $BuildDirectory 'L06A_1.88.221_hybrid-assistant_final.squashfs'
$expectedToolHash = '2EE4A5C87ACBF8A2A2C96B4CA25A40FBA105ADE4752EF112682F63ECB1947277'
$expectedImageHash = '2BA23F9BDEBE5E94F8DBFAA75BA8A8B19643A644ABBCE4A2153436057D1B0801'
$expectedBytes = 40402944
$readLength = '0x2688000'
$stamp = Get-Date -Format 'yyyyMMdd-HHmmss'
$readback = Join-Path $BuildDirectory "system1-final-readback-$stamp.img"
$log = Join-Path $BuildDirectory "system1-final-flash-$stamp.log"

function Assert-FileHash {
    param([string]$Path, [string]$Expected)
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "文件不存在：$Path"
    }
    $actual = (Get-FileHash -LiteralPath $Path -Algorithm SHA256).Hash
    if ($actual -ne $Expected) {
        throw "SHA-256 不匹配：$Path`n期望：$Expected`n实际：$actual"
    }
}

function Invoke-Update {
    param([string[]]$UpdateArgs)
    Push-Location (Split-Path -Parent $tool)
    try {
        $output = & $tool @UpdateArgs 2>&1 | Tee-Object -FilePath $log -Append
        $exitCode = $LASTEXITCODE
        if ($exitCode -ne 0) {
            throw "update.exe 退出码 $exitCode：$($UpdateArgs -join ' ')"
        }
        return ($output | Out-String)
    }
    finally {
        Pop-Location
    }
}

function Wait-AmlogicDebugDevice {
    $deadline = [DateTime]::UtcNow.AddSeconds($CaptureTimeoutSec)
    $attempt = 0
    $lastOutput = ''
    Write-Host 'USB 保持连接、12V 先断开；现在接通 12V。脚本正在连续枚举…' -ForegroundColor Yellow
    Push-Location (Split-Path -Parent $tool)
    try {
        while ([DateTime]::UtcNow -lt $deadline) {
            $attempt++
            $raw = @(& $tool identify 2>&1)
            $exitCode = $LASTEXITCODE
            $lastOutput = ($raw | Out-String)
            if ($exitCode -eq 0 -and $lastOutput -match 'AmlUsbIdentifyHost|0-7-0-16') {
                "识别成功：$(Get-Date -Format o)，尝试次数：$attempt" | Add-Content -LiteralPath $log
                $lastOutput | Add-Content -LiteralPath $log
                Write-Host "已捕获 USB 调试模式（第 $attempt 次枚举）。" -ForegroundColor Green
                return
            }
            if (($attempt % 10) -eq 0) {
                Write-Host "仍在等待 USB 调试模式… 已枚举 $attempt 次"
            }
            if ($PollIntervalMilliseconds -gt 0) {
                Start-Sleep -Milliseconds $PollIntervalMilliseconds
            }
        }
    }
    finally {
        Pop-Location
    }
    "识别超时：$(Get-Date -Format o)，尝试次数：$attempt`n$lastOutput" | Add-Content -LiteralPath $log
    throw "在 $CaptureTimeoutSec 秒内没有捕获到 USB 调试模式；未执行任何分区写入。"
}

Assert-FileHash -Path $tool -Expected $expectedToolHash
Assert-FileHash -Path $image -Expected $expectedImageHash
if ((Get-Item -LiteralPath $image).Length -ne $expectedBytes) {
    throw '镜像长度不匹配，拒绝写入。'
}
if ($PreflightOnly) {
    Write-Host '预检通过：工具、镜像 SHA-256 和镜像长度均正确；未连接或写入设备。' -ForegroundColor Green
    return
}

"开始时间：$(Get-Date -Format o)" | Set-Content -LiteralPath $log -Encoding utf8
"工具：$tool" | Add-Content -LiteralPath $log
"镜像：$image" | Add-Content -LiteralPath $log
"镜像 SHA-256：$expectedImageHash" | Add-Content -LiteralPath $log

Write-Host '1/3 连续枚举并捕获 L06A USB 调试模式…' -ForegroundColor Cyan
Wait-AmlogicDebugDevice

Write-Host '2/3 仅写入 system1；不要断电或拔 USB…' -ForegroundColor Yellow
Invoke-Update -UpdateArgs @('partition', 'system1', $image) | Out-Null

Write-Host '3/3 回读同等长度并核对 SHA-256…' -ForegroundColor Cyan
Invoke-Update -UpdateArgs @('mread', 'store', 'system1', 'normal', $readLength, $readback) | Out-Null
if (-not (Test-Path -LiteralPath $readback -PathType Leaf)) {
    throw '回读文件未生成，停止。'
}
if ((Get-Item -LiteralPath $readback).Length -ne $expectedBytes) {
    throw "回读长度错误：$((Get-Item -LiteralPath $readback).Length)，期望 $expectedBytes。"
}
Assert-FileHash -Path $readback -Expected $expectedImageHash

Write-Host 'system1 写入和完整回读校验通过。' -ForegroundColor Green
Write-Host "SHA-256: $expectedImageHash"
Write-Host "回读文件: $readback"
Write-Host '未修改 bootloader、boot0、boot1、system0、data 或 U-Boot 环境。'
Write-Host '现在断开 12V 和 USB，再只接 12V 正常启动。' -ForegroundColor Green
