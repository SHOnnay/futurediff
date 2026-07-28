$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
Push-Location $Root
try {
  python -m py_compile tools/futurediff_assurance.py tools/futurediff_operations.py tools/futurediff_promotion.py tests/test_assurance.py tests/test_operations.py tests/test_promotion.py
  python -m unittest discover -s tests -p test_*.py -v
  python tools/futurediff_assurance.py secret-scan --root .
  python tools/futurediff_assurance.py license-scan --root . --policy config/license-policy.json
  python tools/futurediff_assurance.py recovery-drill
  python tools/futurediff_assurance.py chaos-run
  python tools/futurediff_assurance.py readiness --root . --policy config/production-readiness-policy.json
  ./scripts/Operations-Assurance.ps1 -OutputDirectory dist/operations-windows
} finally {
  Pop-Location
}
