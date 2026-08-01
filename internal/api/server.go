package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/drain"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/openapispec"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/operatoraudit"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/ratelimit"
	"github.com/SHOnnay/futurediff/internal/requestid"
	"github.com/SHOnnay/futurediff/internal/storageguard"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type Server struct {
	Service                *app.Service
	SocketPath             string
	HTTP                   *http.Server
	Maintenance            *maintenance.Manager
	Drain                  *drain.Manager
	RequirePeerCredentials bool
	AllowedPeerUIDs        map[uint32]struct{}
	RateLimiter            *ratelimit.Limiter
	StorageGuard           *storageguard.Guard
	Authorizer             *authorization.Authorizer
	CapabilityKeyring      *operatorapproval.Keyring
	OperatorAudit          *operatoraudit.Store
	idempotencyMu          sync.Mutex
}

type errorBody struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func writeErr(w http.ResponseWriter, status int, code string, err error) {
	writeJSON(w, status, errorBody{Error: code, Message: err.Error()})
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		status := maintenance.State{Version: maintenance.Version, Enabled: false}
		if s.Maintenance != nil {
			if current, err := s.Maintenance.Status(time.Now()); err == nil {
				status = current
			} else {
				writeErr(w, 503, "maintenance_state_failed", err)
				return
			}
		}
		writeJSON(w, 200, map[string]any{"status": "ok", "implementation": "go", "build": buildinfo.Current(), "time": time.Now().UTC(), "oci": s.Service.RuntimeStatus(r.Context()), "credentials": s.Service.CredentialStatus(), "approvals": s.Service.ApprovalStatus(), "quotas": s.Service.QuotaStatus(), "secret_scan": s.Service.SecretScanStatus(), "peer_auth": map[string]any{"required": s.RequirePeerCredentials, "allowed_uid_count": len(s.AllowedPeerUIDs)}, "authorization": s.authorizationStatus(), "rate_limit": s.rateStatus(), "storage": s.storageStatus(), "maintenance": status, "drain": s.drainStatus()})
	})
	mux.HandleFunc("GET /v1/contract", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, apicontract.Current()) })
	mux.HandleFunc("GET /v1/openapi", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, openapispec.Generate(apicontract.Current()))
	})
	mux.HandleFunc("POST /v1/transactions", s.create)
	mux.HandleFunc("GET /v1/transactions", s.listTransactions)
	mux.HandleFunc("GET /v1/transactions/{id}", s.get)
	mux.HandleFunc("POST /v1/transactions/{id}/execute", s.execute)
	mux.HandleFunc("POST /v1/transactions/{id}/effects/github/branch", s.prepareGitHubBranch)
	mux.HandleFunc("POST /v1/transactions/{id}/effects/github/draft-pull-request", s.prepareGitHubDraftPR)
	mux.HandleFunc("POST /v1/transactions/{id}/effects/slack/message", s.prepareSlackMessage)
	mux.HandleFunc("GET /v1/transactions/{id}/effects", s.effects)
	mux.HandleFunc("POST /v1/transactions/{id}/effects/{effectID}/refresh", s.refreshEffect)
	mux.HandleFunc("POST /v1/transactions/{id}/seal", s.seal)
	mux.HandleFunc("POST /v1/transactions/{id}/verify", s.verify)
	mux.HandleFunc("GET /v1/transactions/{id}/approval-material", s.approvalMaterial)
	mux.HandleFunc("POST /v1/transactions/{id}/approve", s.approve)
	mux.HandleFunc("POST /v1/transactions/{id}/commit", s.commit)
	mux.HandleFunc("POST /v1/transactions/{id}/recover", s.recover)
	mux.HandleFunc("POST /v1/transactions/{id}/abort", s.abort)
	mux.HandleFunc("GET /v1/transactions/{id}/events", s.events)
	mux.HandleFunc("GET /v1/transactions/{id}/access", s.listTransactionAccess)
	mux.HandleFunc("PUT /v1/transactions/{id}/access/{principalID}", s.grantTransactionAccess)
	mux.HandleFunc("DELETE /v1/transactions/{id}/access/{principalID}", s.revokeTransactionAccess)
	return s.requestIDGuard(s.logging(s.peerGuard(s.authorizationGuard(s.rateGuard(s.drainGuard(s.maintenanceGuard(s.storageGuard(s.idempotencyGuard(mux)))))))))
}

func (s *Server) storageStatus() any {
	if s.StorageGuard == nil {
		return map[string]any{"enabled": false}
	}
	status, err := s.StorageGuard.Status(time.Now())
	if err != nil {
		return map[string]any{"enabled": true, "healthy": false, "error": err.Error()}
	}
	return map[string]any{"enabled": true, "status": status}
}

func (s *Server) storageGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || s.StorageGuard == nil {
			next.ServeHTTP(w, r)
			return
		}
		status, err := s.StorageGuard.Status(time.Now())
		if err != nil {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "storage_guard.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: mergeMetadata(map[string]string{"reason": "storage_check_failed"}, safeErrorMetadata(err))}) {
				return
			}
			writeErr(w, http.StatusInsufficientStorage, "storage_check_failed", err)
			return
		}
		if !status.Healthy {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "storage_guard.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "storage_pressure", "finding_count": fmt.Sprint(len(status.Findings))}}) {
				return
			}
			writeJSON(w, http.StatusInsufficientStorage, map[string]any{"error": "storage_pressure", "message": "mutations are blocked by the storage-pressure policy", "storage": status})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) rateStatus() any {
	if s.RateLimiter == nil {
		return map[string]any{"enabled": false}
	}
	status := s.RateLimiter.Status()
	return map[string]any{"enabled": true, "policy": status}
}

func (s *Server) rateGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.RateLimiter == nil {
			next.ServeHTTP(w, r)
			return
		}
		mutation := r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions
		principal := peerauth.Principal(r.Context())
		release, retry, err := s.RateLimiter.Begin(principal, mutation, time.Now())
		if err != nil {
			seconds := int(retry.Round(time.Second) / time.Second)
			if seconds < 1 {
				seconds = 1
			}
			w.Header().Set("Retry-After", fmt.Sprint(seconds))
			if s.Service != nil && s.Service.Ledger != nil {
				_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusTooManyRequests, "", "", requestid.From(r.Context()))
			}
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "rate_limit.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "rate_limited", "retry_after_seconds": fmt.Sprint(seconds)}}) {
				return
			}
			writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "rate_limited", "message": err.Error(), "retry_after_seconds": seconds})
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) peerGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.RequirePeerCredentials {
			next.ServeHTTP(w, r)
			return
		}
		identity, ok := peerauth.FromContext(r.Context())
		if !ok {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "peer_auth.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "peer_credentials_unavailable"}}) {
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "peer_credentials_unavailable", "message": "kernel-authenticated Unix peer credentials are required"})
			return
		}
		if _, allowed := s.AllowedPeerUIDs[identity.UID]; !allowed {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "peer_auth.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "peer_not_authorized"}}) {
				return
			}
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "peer_not_authorized", "message": "Unix peer UID is not authorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) drainStatus() drain.Status {
	if s.Drain == nil {
		return drain.Status{}
	}
	return s.Drain.Status()
}
func (s *Server) drainGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || s.Drain == nil {
			next.ServeHTTP(w, r)
			return
		}
		release, err := s.Drain.BeginMutation()
		if err != nil {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "drain.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "daemon_draining"}}) {
				return
			}
			writeJSON(w, 503, map[string]any{"error": "daemon_draining", "message": err.Error(), "drain": s.Drain.Status()})
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
func (s *Server) maintenanceGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || s.Maintenance == nil {
			next.ServeHTTP(w, r)
			return
		}
		allowed, state, err := s.Maintenance.MutationsAllowed(time.Now())
		if err != nil {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "maintenance.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: mergeMetadata(map[string]string{"reason": "maintenance_state_failed"}, safeErrorMetadata(err))}) {
				return
			}
			writeErr(w, 503, "maintenance_state_failed", err)
			return
		}
		if !allowed {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "maintenance.denied", Target: auditTargetForRequest(r), Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "maintenance_mode", "maintenance_enabled": fmt.Sprint(state.Enabled)}}) {
				return
			}
			writeJSON(w, 503, map[string]any{"error": "maintenance_mode", "message": "mutations are disabled while FutureDiff is in maintenance mode", "maintenance": state})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) Serve() error {
	if s.SocketPath == "" {
		return errors.New("socket path required")
	}
	if err := os.MkdirAll(filepath.Dir(s.SocketPath), 0o700); err != nil {
		return err
	}
	_ = os.Remove(s.SocketPath)
	ln, err := net.Listen("unix", s.SocketPath)
	if err != nil {
		return err
	}
	if err := os.Chmod(s.SocketPath, 0o600); err != nil {
		_ = ln.Close()
		return err
	}
	s.HTTP = &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    32 << 10,
		ConnContext: func(ctx context.Context, conn net.Conn) context.Context {
			identity, err := peerauth.FromConn(conn)
			if err != nil {
				return ctx
			}
			return peerauth.WithIdentity(ctx, identity)
		},
	}
	defer os.Remove(s.SocketPath)
	err = s.HTTP.Serve(ln)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
func (s *Server) DrainAndClose(ctx context.Context, reason string) error {
	if s.Drain != nil {
		s.Drain.Start(reason, time.Now())
		if err := s.Drain.Wait(ctx); err != nil {
			if s.HTTP != nil {
				_ = s.HTTP.Close()
			}
			return fmt.Errorf("drain timed out: %w", err)
		}
	}
	if s.HTTP == nil {
		return nil
	}
	return s.HTTP.Shutdown(ctx)
}
func (s *Server) Close() error {
	if s.HTTP == nil {
		return nil
	}
	return s.HTTP.Close()
}
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	data, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxRequestBodyBytes {
		return errors.New("request body exceeds 1 MiB")
	}
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return err
	}
	var extra any
	if err := d.Decode(&extra); err != io.EOF {
		if err == nil {
			return errors.New("trailing JSON value rejected")
		}
		return err
	}
	return nil
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req app.CreateRequest
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{EventType: "transaction.create.result", Target: operatoraudit.Target{ResourceType: "repository"}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	requestMetadata := mergeMetadata(repoMetadata(req.Repository), map[string]string{"mode": req.Mode, "policy_version": req.PolicyVersion, "dirty_policy": req.DirtyPolicy})
	if !s.auditRequired(w, r, operatoraudit.Input{EventType: "transaction.create.request", Target: operatoraudit.Target{ResourceType: "repository", ResourceID: digestIdentifier("repo", req.Repository)}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: requestMetadata}) {
		return
	}
	v, err := s.Service.CreateForPrincipal(req, peerauth.Principal(r.Context()))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{EventType: "transaction.create.result", Target: operatoraudit.Target{ResourceType: "repository", ResourceID: digestIdentifier("repo", req.Repository)}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(requestMetadata, safeErrorMetadata(err))})
		writeErr(w, 409, "create_failed", err)
		return
	}
	transactionID := v.Transaction.ID
	metadata := mergeMetadata(requestMetadata, map[string]string{"transaction_status": string(v.Transaction.Status)})
	if v.Workspace.SourceHeadRef != "" {
		metadata = mergeMetadata(metadata, map[string]string{"source_head_ref": v.Workspace.SourceHeadRef})
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: transactionID, EventType: "transaction.create.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: transactionID}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata})
	writeJSON(w, 201, v)
}

func (s *Server) listTransactions(w http.ResponseWriter, r *http.Request) {
	decision, _ := authorizationDecisionFromContext(r.Context())
	all := decision.ResourceScope == "all"
	v, err := s.Service.Ledger.ListTransactionsForPrincipal(peerauth.Principal(r.Context()), all)
	if err != nil {
		writeErr(w, 500, "transaction_list_failed", err)
		return
	}
	writeJSON(w, 200, map[string]any{"transactions": v, "resource_scope": decision.ResourceScope})
}

func (s *Server) listTransactionAccess(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.Ledger.ListTransactionGrants(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "transaction_access_failed", err)
		return
	}
	writeJSON(w, 200, map[string]any{"transaction_id": r.PathValue("id"), "grants": v})
}

func (s *Server) grantTransactionAccess(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Permission string `json:"permission"`
	}
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.grant.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	metadata := map[string]string{"subject_principal_id": r.PathValue("principalID"), "permission": req.Permission}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.grant.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	decision, _ := authorizationDecisionFromContext(r.Context())
	err := s.Service.Ledger.GrantTransactionAccess(r.PathValue("id"), peerauth.Principal(r.Context()), r.PathValue("principalID"), ledger.TransactionAccess(req.Permission), decision.ResourceScope == "all", requestid.From(r.Context()))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.grant.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "transaction_access_grant_failed", err)
		return
	}
	v, _ := s.Service.Ledger.ListTransactionGrants(r.PathValue("id"))
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.grant.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata})
	writeJSON(w, 200, map[string]any{"transaction_id": r.PathValue("id"), "grants": v})
}

func (s *Server) revokeTransactionAccess(w http.ResponseWriter, r *http.Request) {
	metadata := map[string]string{"subject_principal_id": r.PathValue("principalID")}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.revoke.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	decision, _ := authorizationDecisionFromContext(r.Context())
	err := s.Service.Ledger.RevokeTransactionAccess(r.PathValue("id"), peerauth.Principal(r.Context()), r.PathValue("principalID"), decision.ResourceScope == "all", requestid.From(r.Context()))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.revoke.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "transaction_access_revoke_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.access.revoke.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata})
	writeJSON(w, 200, map[string]any{"transaction_id": r.PathValue("id"), "revoked_principal_id": r.PathValue("principalID")})
}

func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	var req app.ExecuteRequest
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.execute.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	metadata := map[string]string{"argv0": filepath.Base(firstCommand(req.Command)), "argv_count": fmt.Sprint(len(req.Command)), "env_count": fmt.Sprint(len(req.Environment))}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.execute.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	v, err := s.Service.Execute(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.execute.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "execution_failed", err)
		return
	}
	resultMetadata := mergeMetadata(metadata, map[string]string{"execution_id": v.Execution.ExecutionID, "exit_code": fmt.Sprint(v.Execution.ExitCode), "runtime_kind": v.Execution.RuntimeKind})
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.execute.result", Target: operatoraudit.Target{ResourceType: "execution", ResourceID: v.Execution.ExecutionID}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: resultMetadata})
	writeJSON(w, 200, v)
}

func (s *Server) prepareGitHubBranch(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareGitHubBranchRequest
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_branch.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	metadata := map[string]string{"credential_id": req.CredentialID, "owner": req.Owner, "repo": req.Repo, "branch": req.Branch}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_branch.prepare.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	v, err := s.Service.PrepareGitHubBranch(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_branch.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
	resultMetadata := mergeMetadata(metadata, map[string]string{"effect_id": v.EffectID})
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_branch.prepare.result", Target: operatoraudit.Target{ResourceType: "effect", ResourceID: v.EffectID}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: resultMetadata})
	writeJSON(w, 201, v)
}

func (s *Server) prepareGitHubDraftPR(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareGitHubDraftPRRequest
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_pr.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	metadata := map[string]string{"credential_id": req.CredentialID, "owner": req.Input.Owner, "repo": req.Input.Repo, "head": req.Input.Head, "base": req.Input.Base, "title": digestIdentifier("title", req.Input.Title)}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_pr.prepare.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	v, err := s.Service.PrepareGitHubDraftPR(r.Context(), r.PathValue("id"), req)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_pr.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
	resultMetadata := mergeMetadata(metadata, map[string]string{"effect_id": v.EffectID})
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.github_pr.prepare.result", Target: operatoraudit.Target{ResourceType: "effect", ResourceID: v.EffectID}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: resultMetadata})
	writeJSON(w, 201, v)
}

func (s *Server) prepareSlackMessage(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareSlackMessageRequest
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.slack_message.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	metadata := map[string]string{"credential_id": req.CredentialID, "channel": digestIdentifier("channel", req.Input.Channel), "depends_on_count": fmt.Sprint(len(req.Input.DependsOn))}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.slack_message.prepare.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	v, err := s.Service.PrepareSlackMessage(r.PathValue("id"), req)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.slack_message.prepare.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
	resultMetadata := mergeMetadata(metadata, map[string]string{"effect_id": v.EffectID})
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "effect.slack_message.prepare.result", Target: operatoraudit.Target{ResourceType: "effect", ResourceID: v.EffectID}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: resultMetadata})
	writeJSON(w, 201, v)
}

func (s *Server) effects(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.ExternalEffects(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "effects_failed", err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) refreshEffect(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.RefreshGitHubEffect(r.Context(), r.PathValue("id"), r.PathValue("effectID"))
	if err != nil {
		writeErr(w, 409, "effect_refresh_failed", err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.Get(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "not_found", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) seal(w http.ResponseWriter, r *http.Request) {
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.seal.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow}) {
		return
	}
	v, err := s.Service.Seal(r.PathValue("id"))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.seal.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: safeErrorMetadata(err)})
		writeErr(w, 409, "seal_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.seal.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: map[string]string{"transaction_status": string(v.Transaction.Status)}})
	writeJSON(w, 200, v)
}

func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var c verification.Contract
	if err := decode(r, &c); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.verify.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_contract", err)
		return
	}
	metadata := map[string]string{"contract_id": c.ContractID, "policy_version": c.PolicyVersion, "check_count": fmt.Sprint(len(c.Checks))}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.verify.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	v, err := s.Service.Verify(r.PathValue("id"), c)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.verify.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "verification_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.verify.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, map[string]string{"transaction_status": string(v.Transaction.Status)})})
	writeJSON(w, 200, v)
}

func (s *Server) approvalMaterial(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.ApprovalMaterial(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "approval_material_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Digest   string                     `json:"transaction_digest,omitempty"`
		Approver string                     `json:"approver,omitempty"`
		Envelope *operatorapproval.Envelope `json:"approval_envelope,omitempty"`
		Bundle   *operatorapproval.Bundle   `json:"approval_bundle,omitempty"`
	}
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.approve.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	mode := "digest"
	if req.Envelope != nil {
		mode = "signed-envelope"
	}
	if req.Bundle != nil {
		mode = "signed-bundle"
	}
	metadata := map[string]string{"approval_mode": mode, "approver": req.Approver}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.approve.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow, Metadata: metadata}) {
		return
	}
	var v app.TransactionView
	var err error
	if req.Envelope != nil && req.Bundle != nil {
		err = errors.New("approval envelope and bundle are mutually exclusive")
	} else if req.Bundle != nil {
		v, err = s.Service.ApproveSignedQuorum(r.PathValue("id"), *req.Bundle)
	} else if req.Envelope != nil {
		v, err = s.Service.ApproveSigned(r.PathValue("id"), *req.Envelope)
	} else {
		v, err = s.Service.Approve(r.PathValue("id"), req.Digest, req.Approver)
	}
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.approve.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, safeErrorMetadata(err))})
		writeErr(w, 409, "approval_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.approve.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: mergeMetadata(metadata, map[string]string{"transaction_status": string(v.Transaction.Status)})})
	writeJSON(w, 200, v)
}

func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Digest string `json:"transaction_digest"`
	}
	if err := decode(r, &req); err != nil {
		_ = s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.commit.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyDeny, Metadata: safeErrorMetadata(err)})
		writeErr(w, 400, "invalid_request", err)
		return
	}
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.commit.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow}) {
		return
	}
	v, err := s.Service.CommitContext(r.Context(), r.PathValue("id"), req.Digest)
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.commit.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: safeErrorMetadata(err)})
		writeErr(w, 409, "commit_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.commit.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: map[string]string{"transaction_status": string(v.Transaction.Status), "receipt_count": fmt.Sprint(len(v.Receipts)), "effect_count": fmt.Sprint(len(v.Effects))}})
	writeJSON(w, 200, v)
}

func (s *Server) recover(w http.ResponseWriter, r *http.Request) {
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.recover.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow}) {
		return
	}
	v, err := s.Service.Recover(r.PathValue("id"))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.recover.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: safeErrorMetadata(err)})
		writeErr(w, 409, "recovery_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.recover.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: map[string]string{"transaction_status": string(v.Transaction.Status)}})
	writeJSON(w, 200, v)
}

func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	if !s.auditRequired(w, r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.abort.request", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultRequested, PolicyDecision: operatoraudit.PolicyAllow}) {
		return
	}
	v, err := s.Service.Abort(r.PathValue("id"))
	if err != nil {
		s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.abort.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultFailed, PolicyDecision: operatoraudit.PolicyAllow, Metadata: safeErrorMetadata(err)})
		writeErr(w, 409, "abort_failed", err)
		return
	}
	s.auditBestEffort(r, operatoraudit.Input{TransactionID: r.PathValue("id"), EventType: "transaction.abort.result", Target: operatoraudit.Target{ResourceType: "transaction", ResourceID: r.PathValue("id")}, Result: operatoraudit.ResultSucceeded, PolicyDecision: operatoraudit.PolicyAllow, Metadata: map[string]string{"transaction_status": string(v.Transaction.Status)}})
	writeJSON(w, 200, v)
}
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.Events(r.PathValue("id"))
	if err != nil {
		writeErr(w, 404, "events_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			if !s.auditRequired(w, r, operatoraudit.Input{EventType: "api.invalid_path.denied", Target: operatoraudit.Target{ResourceType: "api_path", ResourceID: r.URL.Path}, Result: operatoraudit.ResultDenied, PolicyDecision: operatoraudit.PolicyDeny, Metadata: map[string]string{"reason": "path_traversal_rejected"}}) {
				return
			}
			writeErr(w, 400, "invalid_path", fmt.Errorf("path traversal rejected"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
