package api

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/requestid"
)

func (s *Server) auditRequired(w http.ResponseWriter, r *http.Request, input operatoraudit.Input) bool {
	if err := s.auditEvent(r, input); err != nil {
		writeErr(w, http.StatusServiceUnavailable, "operator_audit_failed", err)
		return false
	}
	return true
}

func (s *Server) auditBestEffort(r *http.Request, input operatoraudit.Input) {
	if err := s.auditEvent(r, input); err != nil {
		log.Printf("operator audit trail write failed: %v", err)
	}
}

func (s *Server) auditEvent(r *http.Request, input operatoraudit.Input) error {
	if s.OperatorAudit == nil {
		return nil
	}
	if input.OperationID == "" {
		input.OperationID = requestid.From(r.Context())
	}
	if input.Actor.Source == "" {
		input.Actor = auditActor(r)
	}
	if input.Context.Component == "" {
		input.Context = operatoraudit.ExecutionContext{
			Component: "api",
			RequestID: requestid.From(r.Context()),
			Method:    r.Method,
			Path:      r.URL.Path,
		}
	}
	_, err := s.OperatorAudit.Record(input)
	return err
}

func auditActor(r *http.Request) operatoraudit.Actor {
	actor := operatoraudit.Actor{PrincipalID: peerauth.Principal(r.Context()), Source: "local"}
	if identity, ok := peerauth.FromContext(r.Context()); ok {
		actor.PeerUID = identity.UID
		actor.Source = "unix-peer"
	}
	return actor
}

func safeErrorMetadata(err error) map[string]string {
	if err == nil {
		return nil
	}
	return map[string]string{"error": operatoraudit.RedactText(err.Error())}
}

func mergeMetadata(parts ...map[string]string) map[string]string {
	merged := map[string]string{}
	for _, part := range parts {
		for key, value := range part {
			merged[key] = value
		}
	}
	if len(merged) == 0 {
		return nil
	}
	return merged
}

func digestIdentifier(kind, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(value))
	return fmt.Sprintf("%s:sha256:%s", kind, hex.EncodeToString(sum[:6]))
}

func repoMetadata(path string) map[string]string {
	return map[string]string{"repository": digestIdentifier("repo", path)}
}

func firstCommand(command []string) string {
	if len(command) == 0 {
		return ""
	}
	return command[0]
}

func auditTargetForRequest(r *http.Request) operatoraudit.Target {
	matched, ok := apicontract.Match(r.Method, r.URL.Path)
	if !ok {
		return operatoraudit.Target{ResourceType: "api_path", ResourceID: r.URL.Path}
	}
	if id := matched.Params["id"]; id != "" {
		return operatoraudit.Target{ResourceType: "transaction", ResourceID: id}
	}
	return operatoraudit.Target{ResourceType: "api_operation", ResourceID: matched.Endpoint.OperationID}
}
