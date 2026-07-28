param(
  [Parameter(Mandatory=$true)][string]$CanonicalRepository,
  [Parameter(Mandatory=$true)][string]$BaseArchive,
  [Parameter(Mandatory=$true)][string]$ArchiveDirectory,
  [Parameter(Mandatory=$true)][string]$OutputDirectory
)
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
python "$Root/tools/futurediff_closure.py" --help | Out-Null
Write-Host "Run scripts/production-closure.sh on Linux/macOS, or invoke the same futurediff_closure.py subcommands from PowerShell with production evidence paths."
exit 0
