param(
    [string]$TaskName = "RelayCentralizerEdge"
)

$ErrorActionPreference = "Stop"

$existingTask = Get-ScheduledTask -TaskName $TaskName -ErrorAction SilentlyContinue
if ($null -ne $existingTask) {
    Unregister-ScheduledTask -TaskName $TaskName -Confirm:$false
}
