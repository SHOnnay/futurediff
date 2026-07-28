param(
    [Parameter(Mandatory=$true)][string]$EvidenceRoot,
    [Parameter(Mandatory=$true)][string]$EvidenceSpecification,
    [Parameter(Mandatory=$true)][string]$Claims,
    [Parameter(Mandatory=$true)][string]$Candidate,
    [Parameter(Mandatory=$true)][string]$Approvals,
    [Parameter(Mandatory=$true)][string]$OutputDirectory
)
$ErrorActionPreference = "Stop"
$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null
$Now = [DateTime]::UtcNow.ToString("yyyy-MM-ddTHH:mm:ssZ")
python "$Root/tools/futurediff_promotion.py" evidence-intake --root $EvidenceRoot --specification $EvidenceSpecification --policy "$Root/config/external-evidence-policy.json" --now $Now --output "$OutputDirectory/external-evidence-intake.json"
python "$Root/tools/futurediff_promotion.py" oidc-claims-verify --claims $Claims --policy "$Root/config/hosted-identity-policy.json" --now $Now --output "$OutputDirectory/hosted-identity.json"
python "$Root/tools/futurediff_promotion.py" promotion-evaluate --candidate $Candidate --intake "$OutputDirectory/external-evidence-intake.json" --identity "$OutputDirectory/hosted-identity.json" --approvals $Approvals --policy "$Root/config/promotion-policy.json" --output "$OutputDirectory/promotion-decision.json"
Write-Output "RELEASE PROMOTION PASS"
