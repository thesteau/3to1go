$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$configRoot = Join-Path $env:ProgramData "RelayCentralizerEdge"
$envFile = Join-Path $configRoot "edge.env"
$sampleFile = Join-Path $configRoot "edge.env.sample"

New-Item -ItemType Directory -Path $configRoot -Force | Out-Null

if (-not (Test-Path $envFile) -and (Test-Path $sampleFile)) {
    Copy-Item -Path $sampleFile -Destination $envFile
}

$env:EDGE_ENV_FILE = $envFile
& (Join-Path $scriptDir "relaycentralizer-edge.exe")
