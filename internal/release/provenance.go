package release

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/SHOnnay/futurediff/internal/buildinfo"
)

type InTotoStatement struct {
	Type          string         `json:"_type"`
	Subject       []Subject      `json:"subject"`
	PredicateType string         `json:"predicateType"`
	Predicate     SLSAProvenance `json:"predicate"`
}

type Subject struct {
	Name   string            `json:"name"`
	Digest map[string]string `json:"digest"`
}

type SLSAProvenance struct {
	BuildDefinition BuildDefinition `json:"buildDefinition"`
	RunDetails      RunDetails      `json:"runDetails"`
}

type BuildDefinition struct {
	BuildType            string         `json:"buildType"`
	ExternalParameters   map[string]any `json:"externalParameters"`
	InternalParameters   map[string]any `json:"internalParameters"`
	ResolvedDependencies []Resource     `json:"resolvedDependencies,omitempty"`
}

type Resource struct {
	URI    string            `json:"uri"`
	Digest map[string]string `json:"digest,omitempty"`
}

type RunDetails struct {
	Builder  Builder  `json:"builder"`
	Metadata Metadata `json:"metadata"`
}

type Builder struct {
	ID string `json:"id"`
}
type Metadata struct {
	InvocationID string    `json:"invocationId,omitempty"`
	StartedOn    time.Time `json:"startedOn"`
	FinishedOn   time.Time `json:"finishedOn"`
}

type ProvenanceOptions struct {
	Artifacts    []string
	BuilderID    string
	InvocationID string
	SourceURI    string
	SourceDigest string
	StartedOn    time.Time
	FinishedOn   time.Time
}

func GenerateProvenance(o ProvenanceOptions) (InTotoStatement, error) {
	if len(o.Artifacts) == 0 {
		return InTotoStatement{}, errors.New("at least one artifact is required")
	}
	if o.BuilderID == "" {
		o.BuilderID = "https://futurediff.dev/builders/local-go/v1"
	}
	if o.StartedOn.IsZero() {
		o.StartedOn = time.Now().UTC()
	}
	if o.FinishedOn.IsZero() {
		o.FinishedOn = o.StartedOn
	}
	subjects := make([]Subject, 0, len(o.Artifacts))
	for _, artifact := range o.Artifacts {
		data, err := os.ReadFile(artifact)
		if err != nil {
			return InTotoStatement{}, err
		}
		sum := sha256.Sum256(data)
		subjects = append(subjects, Subject{Name: filepath.Base(artifact), Digest: map[string]string{"sha256": hex.EncodeToString(sum[:])}})
	}
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].Name < subjects[j].Name })
	info := buildinfo.Current()
	dependencies := []Resource{}
	if o.SourceURI != "" {
		resource := Resource{URI: o.SourceURI}
		if o.SourceDigest != "" {
			resource.Digest = map[string]string{"sha1": o.SourceDigest}
		}
		dependencies = append(dependencies, resource)
	}
	return InTotoStatement{
		Type:          "https://in-toto.io/Statement/v1",
		Subject:       subjects,
		PredicateType: "https://slsa.dev/provenance/v1",
		Predicate: SLSAProvenance{
			BuildDefinition: BuildDefinition{
				BuildType:            "https://futurediff.dev/buildtypes/go-release/v1",
				ExternalParameters:   map[string]any{"version": info.Version, "platform": info.Platform, "module": info.Module},
				InternalParameters:   map[string]any{"go_version": info.GoVersion, "dirty": info.Dirty},
				ResolvedDependencies: dependencies,
			},
			RunDetails: RunDetails{Builder: Builder{ID: o.BuilderID}, Metadata: Metadata{InvocationID: o.InvocationID, StartedOn: o.StartedOn.UTC(), FinishedOn: o.FinishedOn.UTC()}},
		},
	}, nil
}

func WriteProvenance(path string, statement InTotoStatement) error {
	data, err := json.Marshal(statement)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if path == "" || path == "-" {
		_, err = os.Stdout.Write(data)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
