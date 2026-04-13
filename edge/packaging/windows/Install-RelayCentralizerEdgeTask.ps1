param(
    [string]$TaskName = "RelayCentralizerEdge"
)

$ErrorActionPreference = "Stop"

$scriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$programDataRoot = Join-Path $env:ProgramData "RelayCentralizerEdge"
$envFile = Join-Path $programDataRoot "edge.env"
$sampleFile = Join-Path $programDataRoot "edge.env.sample"

New-Item -ItemType Directory -Path $programDataRoot -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $programDataRoot "state") -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $programDataRoot "spool") -Force | Out-Null

if (-not (Test-Path $envFile) -and (Test-Path $sampleFile)) {
    Copy-Item -Path $sampleFile -Destination $envFile
}

$startScript = Join-Path $scriptDir "Start-RelayCentralizerEdge.ps1"
$taskArgs = "-NoProfile -ExecutionPolicy Bypass -File `"$startScript`""
$action = New-ScheduledTaskAction -Execute "powershell.exe" -Argument $taskArgs
$trigger = New-ScheduledTaskTrigger -AtStartup
$principal = New-ScheduledTaskPrincipal -UserId "SYSTEM" -LogonType ServiceAccount -RunLevel Highest
$settings = New-ScheduledTaskSettingsSet -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -StartWhenAvailable
$task = New-ScheduledTask -Action $action -Trigger $trigger -Principal $principal -Settings $settings

Register-ScheduledTask -TaskName $TaskName -InputObject $task -Force | Out-Null
Start-ScheduledTask -TaskName $TaskName
