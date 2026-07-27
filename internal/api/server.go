package api

import (
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
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/app"
	"github.com/SHOnnay/futurediff/internal/buildinfo"
	"github.com/SHOnnay/futurediff/internal/drain"
	"github.com/SHOnnay/futurediff/internal/maintenance"
	"github.com/SHOnnay/futurediff/internal/operatorapproval"
	"github.com/SHOnnay/futurediff/internal/verification"
)

type Server struct {
	Service     *app.Service
	SocketPath  string
	HTTP        *http.Server
	Maintenance *maintenance.Manager
	Drain       *drain.Manager
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
		writeJSON(w, 200, map[string]any{"status": "ok", "implementation": "go", "build": buildinfo.Current(), "time": time.Now().UTC(), "oci": s.Service.RuntimeStatus(r.Context()), "credentials": s.Service.CredentialStatus(), "approvals": s.Service.ApprovalStatus(), "maintenance": status, "drain": s.drainStatus()})
	})
	mux.HandleFunc("GET /v1/contract", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, 200, apicontract.Current()) })
	mux.HandleFunc("POST /v1/transactions", s.create)
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
	return logging(s.drainGuard(s.maintenanceGuard(mux)))
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
			writeErr(w, 503, "maintenance_state_failed", err)
			return
		}
		if !allowed {
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
	s.HTTP = &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 5 * time.Second}
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
	d := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	d.DisallowUnknownFields()
	return d.Decode(v)
}
func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var req app.CreateRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.Create(req)
	if err != nil {
		writeErr(w, 409, "create_failed", err)
		return
	}
	writeJSON(w, 201, v)
}
func (s *Server) execute(w http.ResponseWriter, r *http.Request) {
	var req app.ExecuteRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.Execute(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeErr(w, 409, "execution_failed", err)
		return
	}
	writeJSON(w, 200, v)
}

func (s *Server) prepareGitHubBranch(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareGitHubBranchRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.PrepareGitHubBranch(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
	writeJSON(w, 201, v)
}

func (s *Server) prepareGitHubDraftPR(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareGitHubDraftPRRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.PrepareGitHubDraftPR(r.Context(), r.PathValue("id"), req)
	if err != nil {
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
	writeJSON(w, 201, v)
}

func (s *Server) prepareSlackMessage(w http.ResponseWriter, r *http.Request) {
	var req app.PrepareSlackMessageRequest
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.PrepareSlackMessage(r.PathValue("id"), req)
	if err != nil {
		writeErr(w, 409, "effect_prepare_failed", err)
		return
	}
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
	v, err := s.Service.Seal(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "seal_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) verify(w http.ResponseWriter, r *http.Request) {
	var c verification.Contract
	if err := decode(r, &c); err != nil {
		writeErr(w, 400, "invalid_contract", err)
		return
	}
	v, err := s.Service.Verify(r.PathValue("id"), c)
	if err != nil {
		writeErr(w, 409, "verification_failed", err)
		return
	}
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
		writeErr(w, 400, "invalid_request", err)
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
		writeErr(w, 409, "approval_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) commit(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Digest string `json:"transaction_digest"`
	}
	if err := decode(r, &req); err != nil {
		writeErr(w, 400, "invalid_request", err)
		return
	}
	v, err := s.Service.CommitContext(r.Context(), r.PathValue("id"), req.Digest)
	if err != nil {
		writeErr(w, 409, "commit_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) recover(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.Recover(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "recovery_failed", err)
		return
	}
	writeJSON(w, 200, v)
}
func (s *Server) abort(w http.ResponseWriter, r *http.Request) {
	v, err := s.Service.Abort(r.PathValue("id"))
	if err != nil {
		writeErr(w, 409, "abort_failed", err)
		return
	}
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
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "..") {
			writeErr(w, 400, "invalid_path", fmt.Errorf("path traversal rejected"))
			return
		}
		next.ServeHTTP(w, r)
	})
}
