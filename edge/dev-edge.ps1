param(
    [Parameter(ValueFromRemainingArguments = $true)]
    [string[]]$Arguments
)

$ErrorActionPreference = "Stop"
$scriptRoot = Split-Path -Parent $MyInvocation.MyCommand.Path
$manager = Join-Path $scriptRoot "scripts\dev_edge.py"

if (Get-Command py -ErrorAction SilentlyContinue) {
    & py -3 $manager @Arguments
    exit $LASTEXITCODE
}

if (Get-Command python -ErrorAction SilentlyContinue) {
    & python $manager @Arguments
    exit $LASTEXITCODE
}

Write-Error "Python 3 was not found on PATH. Install Python and try again."
