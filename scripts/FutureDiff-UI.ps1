param([Parameter(ValueFromRemainingArguments = $true)][string[]]$Arguments)
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$Tool = Join-Path $Root "tools/futurediff_cli_ui.py"
& python $Tool @Arguments
exit $LASTEXITCODE
