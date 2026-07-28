param([Parameter(ValueFromRemainingArguments=$true)][string[]]$Arguments)
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
& python "$Root/tools/futurediff_assurance.py" @Arguments
exit $LASTEXITCODE
