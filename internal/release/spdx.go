package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
)

type SPDXDocument struct {
	SPDXVersion       string         `json:"spdxVersion"`
	DataLicense       string         `json:"dataLicense"`
	SPDXID            string         `json:"SPDXID"`
	Name              string         `json:"name"`
	DocumentNamespace string         `json:"documentNamespace"`
	CreationInfo      CreationInfo   `json:"creationInfo"`
	Packages          []SPDXPackage  `json:"packages"`
	Files             []SPDXFile     `json:"files,omitempty"`
	Relationships     []Relationship `json:"relationships,omitempty"`
}

type CreationInfo struct {
	Created  string   `json:"created"`
	Creators []string `json:"creators"`
}

type SPDXPackage struct {
	Name             string `json:"name"`
	SPDXID           string `json:"SPDXID"`
	VersionInfo      string `json:"versionInfo"`
	DownloadLocation string `json:"downloadLocation"`
	FilesAnalyzed    bool   `json:"filesAnalyzed"`
	LicenseConcluded string `json:"licenseConcluded"`
	LicenseDeclared  string `json:"licenseDeclared"`
	CopyrightText    string `json:"copyrightText"`
	Supplier         string `json:"supplier,omitempty"`
}

type SPDXFile struct {
	FileName         string     `json:"fileName"`
	SPDXID           string     `json:"SPDXID"`
	Checksums        []Checksum `json:"checksums"`
	LicenseConcluded string     `json:"licenseConcluded"`
	CopyrightText    string     `json:"copyrightText"`
}

type Checksum struct {
	Algorithm string `json:"algorithm"`
	Value     string `json:"checksumValue"`
}

type Relationship struct {
	SPDXElementID      string `json:"spdxElementId"`
	RelationshipType   string `json:"relationshipType"`
	RelatedSPDXElement string `json:"relatedSpdxElement"`
}

func GenerateSPDX(root string, includeFiles bool) (SPDXDocument, error) {
	info := buildinfo.Current()
	version := info.Version
	if version == "" {
		version = "dev"
	}
	namespaceSeed := sha256.Sum256([]byte(strings.Join([]string{info.Module, version, info.Commit}, "\x00")))
	doc := SPDXDocument{
		SPDXVersion: "SPDX-2.3", DataLicense: "CC0-1.0", SPDXID: "SPDXRef-DOCUMENT",
		Name:              "FutureDiff-" + version,
		DocumentNamespace: "https://futurediff.dev/spdx/" + hex.EncodeToString(namespaceSeed[:]),
		CreationInfo:      CreationInfo{Created: time.Now().UTC().Format(time.RFC3339), Creators: []string{"Tool: futurediff-sbom", "Organization: FutureDiff contributors"}},
		Packages:          []SPDXPackage{{Name: "github.com/SHOnnay/futurediff", SPDXID: "SPDXRef-Package-FutureDiff", VersionInfo: version, DownloadLocation: "NOASSERTION", FilesAnalyzed: includeFiles, LicenseConcluded: "NOASSERTION", LicenseDeclared: "NOASSERTION", CopyrightText: "NOASSERTION", Supplier: "Organization: FutureDiff contributors"}},
		Relationships:     []Relationship{{SPDXElementID: "SPDXRef-DOCUMENT", RelationshipType: "DESCRIBES", RelatedSPDXElement: "SPDXRef-Package-FutureDiff"}},
	}
	if !includeFiles {
		return doc, nil
	}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel == ".git" || rel == "bin" || strings.HasPrefix(rel, "research/original-design-branch/.git") {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(data)
		idSum := sha256.Sum256([]byte(filepath.ToSlash(rel)))
		id := "SPDXRef-File-" + hex.EncodeToString(idSum[:8])
		doc.Files = append(doc.Files, SPDXFile{FileName: "./" + filepath.ToSlash(rel), SPDXID: id, Checksums: []Checksum{{Algorithm: "SHA256", Value: hex.EncodeToString(sum[:])}}, LicenseConcluded: "NOASSERTION", CopyrightText: "NOASSERTION"})
		doc.Relationships = append(doc.Relationships, Relationship{SPDXElementID: "SPDXRef-Package-FutureDiff", RelationshipType: "CONTAINS", RelatedSPDXElement: id})
		return nil
	})
	sort.Slice(doc.Files, func(i, j int) bool { return doc.Files[i].FileName < doc.Files[j].FileName })
	sort.Slice(doc.Relationships[1:], func(i, j int) bool {
		return doc.Relationships[i+1].RelatedSPDXElement < doc.Relationships[j+1].RelatedSPDXElement
	})
	return doc, err
}

func WriteSPDX(path string, document SPDXDocument) error {
	encoded, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(encoded)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, encoded, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func SHA256File(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func WriteChecksums(path string, files []string) error {
	sort.Strings(files)
	var lines []string
	for _, file := range files {
		digest, err := SHA256File(file)
		if err != nil {
			return err
		}
		lines = append(lines, fmt.Sprintf("%s  %s", digest, filepath.Base(file)))
	}
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}
