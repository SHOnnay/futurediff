package secretscan

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

const PolicyVersion = "0.1"

type Severity string

const (
	SeverityHigh   Severity = "high"
	SeverityMedium Severity = "medium"
)

type Rule struct {
	ID       string
	Severity Severity
	Pattern  *regexp.Regexp
	Group    int
}

type Policy struct {
	Version             string     `json:"version"`
	Enabled             bool       `json:"enabled"`
	BlockSeverities     []Severity `json:"block_severities"`
	AllowedFingerprints []string   `json:"allowed_fingerprints,omitempty"`
}

type Finding struct {
	RuleID      string   `json:"rule_id"`
	Severity    Severity `json:"severity"`
	Line        int      `json:"line"`
	Fingerprint string   `json:"fingerprint"`
	Preview     string   `json:"preview"`
}

type Report struct {
	PolicyVersion string    `json:"policy_version"`
	ScannedLines  int       `json:"scanned_lines"`
	Findings      []Finding `json:"findings"`
	Blocking      bool      `json:"blocking"`
	Digest        string    `json:"digest"`
}

type Scanner struct {
	Policy Policy
	Rules  []Rule
}

func DefaultPolicy() Policy {
	return Policy{Version: PolicyVersion, Enabled: true, BlockSeverities: []Severity{SeverityHigh}}
}

func Default() *Scanner {
	return &Scanner{Policy: DefaultPolicy(), Rules: DefaultRules()}
}

func DefaultRules() []Rule {
	return []Rule{
		{ID: "private_key", Severity: SeverityHigh, Pattern: regexp.MustCompile(`-----BEGIN (?:RSA |EC |OPENSSH |DSA )?PRIVATE KEY-----`)},
		{ID: "github_token", Severity: SeverityHigh, Pattern: regexp.MustCompile(`(?:github_pat_[A-Za-z0-9_]{20,}|gh[pousr]_[A-Za-z0-9]{20,})`)},
		{ID: "slack_token", Severity: SeverityHigh, Pattern: regexp.MustCompile(`xox[baprs]-[A-Za-z0-9-]{10,}`)},
		{ID: "aws_access_key", Severity: SeverityHigh, Pattern: regexp.MustCompile(`AKIA[0-9A-Z]{16}`)},
		{ID: "generic_assignment", Severity: SeverityMedium, Pattern: regexp.MustCompile(`(?i)(?:api[_-]?key|access[_-]?token|secret|password|passwd)\s*[:=]\s*["']?([A-Za-z0-9_./+=-]{20,})`), Group: 1},
	}
}

func LoadPolicy(path string) (Policy, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Policy{}, err
	}
	if st.Mode().Perm()&0o022 != 0 {
		return Policy{}, errors.New("secret-scan policy must not be group/world writable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	var policy Policy
	if err := dec.Decode(&policy); err != nil {
		return Policy{}, err
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		return Policy{}, errors.New("trailing JSON rejected")
	}
	return policy, ValidatePolicy(policy)
}

func ValidatePolicy(policy Policy) error {
	if policy.Version != PolicyVersion {
		return fmt.Errorf("unsupported secret-scan policy version %q", policy.Version)
	}
	valid := map[Severity]bool{SeverityHigh: true, SeverityMedium: true}
	for _, severity := range policy.BlockSeverities {
		if !valid[severity] {
			return fmt.Errorf("unsupported blocking severity %q", severity)
		}
	}
	for _, fingerprint := range policy.AllowedFingerprints {
		if len(fingerprint) != 64 {
			return fmt.Errorf("invalid allowed fingerprint %q", fingerprint)
		}
		if _, err := hex.DecodeString(fingerprint); err != nil {
			return fmt.Errorf("invalid allowed fingerprint %q", fingerprint)
		}
	}
	return nil
}

func (s *Scanner) ScanPatchFile(path string) (Report, error) {
	file, err := os.Open(path)
	if err != nil {
		return Report{}, err
	}
	defer file.Close()
	return s.ScanPatch(file)
}

func (s *Scanner) ScanPatch(reader interface{ Read([]byte) (int, error) }) (Report, error) {
	if s == nil {
		return Report{}, errors.New("scanner is nil")
	}
	if err := ValidatePolicy(s.Policy); err != nil {
		return Report{}, err
	}
	if !s.Policy.Enabled {
		report := Report{PolicyVersion: s.Policy.Version}
		report.Digest = reportDigest(report)
		return report, nil
	}
	allowed := map[string]bool{}
	for _, value := range s.Policy.AllowedFingerprints {
		allowed[strings.ToLower(value)] = true
	}
	blockedSeverity := map[Severity]bool{}
	for _, value := range s.Policy.BlockSeverities {
		blockedSeverity[value] = true
	}
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 2<<20)
	lineNo := 0
	var findings []Finding
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		if !strings.HasPrefix(line, "+") || strings.HasPrefix(line, "+++") {
			continue
		}
		content := strings.TrimPrefix(line, "+")
		for _, rule := range s.Rules {
			matches := rule.Pattern.FindAllStringSubmatch(content, -1)
			for _, match := range matches {
				secret := match[0]
				if rule.Group > 0 && rule.Group < len(match) {
					secret = match[rule.Group]
				}
				fingerprint := fingerprint(secret)
				if allowed[fingerprint] {
					continue
				}
				findings = append(findings, Finding{RuleID: rule.ID, Severity: rule.Severity, Line: lineNo, Fingerprint: fingerprint, Preview: redactedPreview(secret)})
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return Report{}, err
	}
	sort.Slice(findings, func(i, j int) bool {
		if findings[i].Line != findings[j].Line {
			return findings[i].Line < findings[j].Line
		}
		if findings[i].RuleID != findings[j].RuleID {
			return findings[i].RuleID < findings[j].RuleID
		}
		return findings[i].Fingerprint < findings[j].Fingerprint
	})
	report := Report{PolicyVersion: s.Policy.Version, ScannedLines: lineNo, Findings: findings}
	for _, finding := range findings {
		if blockedSeverity[finding.Severity] {
			report.Blocking = true
		}
	}
	report.Digest = reportDigest(report)
	return report, nil
}

func fingerprint(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func redactedPreview(value string) string {
	if len(value) <= 8 {
		return "[REDACTED]"
	}
	return value[:4] + "…" + value[len(value)-4:]
}
func reportDigest(report Report) string {
	copyReport := report
	copyReport.Digest = ""
	data, _ := json.Marshal(copyReport)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
