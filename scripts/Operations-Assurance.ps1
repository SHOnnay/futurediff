param([string]$OutputDirectory = "dist/operations")
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Set-Location $Root
python tools/futurediff_operations.py deployment-validate --input config/deployment-contract.json --output "$OutputDirectory/deployment-contract.json"
python tools/futurediff_operations.py environment-parity --input config/deployment-contract.json --policy config/environment-parity-policy.json --output "$OutputDirectory/environment-parity.json"
python tools/futurediff_operations.py compatibility-validate --input examples/compatibility-matrix.example.json --policy config/compatibility-policy.json --output "$OutputDirectory/compatibility-matrix.json"
python tools/futurediff_operations.py upgrade-validate --input examples/upgrade-plan.example.json --output "$OutputDirectory/upgrade-plan.json"
python tools/futurediff_operations.py rollback-drill --input examples/upgrade-plan.example.json --output "$OutputDirectory/rollback-drill.json"
python tools/futurediff_operations.py capacity-evaluate --input examples/capacity-test.example.json --policy config/capacity-policy.json --output "$OutputDirectory/capacity-test.json"
python tools/futurediff_operations.py soak-evaluate --input examples/soak-test.example.json --policy config/soak-policy.json --output "$OutputDirectory/soak-test.json"
python tools/futurediff_operations.py observability-validate --input config/observability-contract.json --policy config/observability-policy.json --output "$OutputDirectory/observability-contract.json"
python tools/futurediff_operations.py alert-routing-validate --input examples/alert-routing.example.json --policy config/alert-routing-policy.json --output "$OutputDirectory/alert-routing.json"
python tools/futurediff_operations.py data-governance-validate --input config/data-governance-policy.json --output "$OutputDirectory/data-governance.json"
python tools/futurediff_operations.py incident-tabletop-evaluate --input examples/incident-tabletop.example.json --policy config/incident-tabletop-policy.json --output "$OutputDirectory/incident-tabletop.json"
python tools/futurediff_operations.py approvals-validate --input examples/release-approvals.example.json --policy config/release-approval-policy.json --output "$OutputDirectory/release-approvals.json"
Write-Output "Core operational checks passed. Use scripts/operations-assurance.sh on Linux/macOS to build the deterministic evidence bundle and final gate."
