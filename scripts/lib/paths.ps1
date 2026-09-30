$ProjectRoot = Split-Path -Parent (Split-Path -Parent $PSScriptRoot)
$LocalDirectory = if ($env:XIAOAI_LOCAL_DIR) { $env:XIAOAI_LOCAL_DIR } else { Join-Path $ProjectRoot 'local' }
$BuildDirectory = if ($env:XIAOAI_BUILD_DIR) { $env:XIAOAI_BUILD_DIR } else { Join-Path $LocalDirectory 'build/L06A_1.88.221' }
$AccessDirectory = if ($env:XIAOAI_ACCESS_DIR) { $env:XIAOAI_ACCESS_DIR } else { Join-Path $BuildDirectory 'network-access' }
