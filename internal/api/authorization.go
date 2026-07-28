package api

import (
	"context"
	"net/http"
	"time"

	"github.com/SHOnnay/futurediff/internal/apicontract"
	"github.com/SHOnnay/futurediff/internal/authorization"
	"github.com/SHOnnay/futurediff/internal/capability"
	"github.com/SHOnnay/futurediff/internal/ledger"
	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/requestid"
)

const capabilityHeader = "X-FutureDiff-Capability"

type authorizationDecisionContextKey struct{}

func withAuthorizationDecision(ctx context.Context, d authorization.Decision) context.Context {
	return context.WithValue(ctx, authorizationDecisionContextKey{}, d)
}
func authorizationDecisionFromContext(ctx context.Context) (authorization.Decision, bool) {
	d, ok := ctx.Value(authorizationDecisionContextKey{}).(authorization.Decision)
	return d, ok
}

func transactionAccessFor(endpoint apicontract.Endpoint) ledger.TransactionAccess {
	switch endpoint.OperationID {
	case "transaction_access_list", "transaction_access_grant", "transaction_access_revoke", "approval_material", "transaction_approve", "transaction_commit", "transaction_recover", "transaction_abort":
		return ledger.AccessAdmin
	}
	if endpoint.Method == http.MethodGet || endpoint.Method == http.MethodHead {
		return ledger.AccessRead
	}
	return ledger.AccessOperate
}

func (s *Server) authorizationGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.Authorizer == nil {
			next.ServeHTTP(w, r)
			return
		}
		matched, ok := apicontract.Match(r.Method, r.URL.Path)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		identity, ok := peerauth.FromContext(r.Context())
		if !ok {
			s.recordAuthorization(r, ledger.AuthorizationDecisionInput{PrincipalID: peerauth.Principal(r.Context()), OperationID: matched.Endpoint.OperationID, Allowed: false, Source: "rbac", ReasonCode: "peer_identity_unavailable", PolicyDigest: s.Authorizer.Digest(), RequestID: requestid.From(r.Context())})
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "authorization_denied", "message": "kernel-authenticated peer identity is required"})
			return
		}
		principal := peerauth.Principal(r.Context())
		resourceID := matched.Params["id"]
		decision := s.Authorizer.Decide(identity.UID, matched.Endpoint.OperationID)
		roleAllowed := decision.Allowed
		resourceAllowed := resourceID == "" || decision.ResourceScope == "all"
		if roleAllowed && !resourceAllowed && s.Service != nil && s.Service.Ledger != nil {
			allowed, err := s.Service.Ledger.CheckTransactionAccess(resourceID, principal, transactionAccessFor(matched.Endpoint))
			resourceAllowed = err == nil && allowed
		}
		if roleAllowed && resourceAllowed {
			reason := decision.ReasonCode
			if resourceID != "" {
				reason = "role_grant_" + decision.ResourceScope
			}
			s.recordAuthorization(r, ledger.AuthorizationDecisionInput{PrincipalID: principal, OperationID: matched.Endpoint.OperationID, ResourceID: resourceID, Allowed: true, Source: "rbac", ReasonCode: reason, PolicyDigest: decision.PolicyDigest, Roles: decision.Roles, RequestID: requestid.From(r.Context())})
			next.ServeHTTP(w, r.WithContext(withAuthorizationDecision(r.Context(), decision)))
			return
		}

		encoded := r.Header.Get(capabilityHeader)
		if !matched.Endpoint.AgentSafe && encoded != "" && s.CapabilityKeyring != nil && s.Service != nil && s.Service.Ledger != nil {
			token, err := capability.DecodeCompact(encoded)
			if err == nil {
				err = capability.Verify(*s.CapabilityKeyring, token, identity.UID, matched.Endpoint.OperationID, resourceID, time.Now())
			}
			if err == nil {
				digest := capability.Digest(token)
				err = s.Service.Ledger.ConsumeAuthorizationCapability(token.CapabilityID, principal, matched.Endpoint.OperationID, resourceID, digest)
				if err == nil {
					r.Header.Del(capabilityHeader)
					capabilityDecision := decision
					capabilityDecision.Allowed = true
					capabilityDecision.ResourceScope = "capability"
					s.recordAuthorization(r, ledger.AuthorizationDecisionInput{PrincipalID: principal, OperationID: matched.Endpoint.OperationID, ResourceID: resourceID, Allowed: true, Source: "capability", ReasonCode: "signed_capability", PolicyDigest: decision.PolicyDigest, Roles: decision.Roles, CapabilityDigest: digest, RequestID: requestid.From(r.Context())})
					next.ServeHTTP(w, r.WithContext(withAuthorizationDecision(r.Context(), capabilityDecision)))
					return
				}
			}
			s.recordAuthorization(r, ledger.AuthorizationDecisionInput{PrincipalID: principal, OperationID: matched.Endpoint.OperationID, ResourceID: resourceID, Allowed: false, Source: "capability", ReasonCode: "capability_rejected", PolicyDigest: decision.PolicyDigest, Roles: decision.Roles, RequestID: requestid.From(r.Context())})
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "authorization_denied", "message": "signed capability was invalid, expired, out of scope, or already used"})
			return
		}
		reason := decision.ReasonCode
		if roleAllowed && !resourceAllowed {
			reason = "resource_scope_denied"
		}
		s.recordAuthorization(r, ledger.AuthorizationDecisionInput{PrincipalID: principal, OperationID: matched.Endpoint.OperationID, ResourceID: resourceID, Allowed: false, Source: "rbac", ReasonCode: reason, PolicyDigest: decision.PolicyDigest, Roles: decision.Roles, RequestID: requestid.From(r.Context())})
		if roleAllowed && !resourceAllowed {
			writeJSON(w, http.StatusNotFound, map[string]any{"error": "transaction_not_found", "message": "transaction is not visible to this principal"})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]any{"error": "authorization_denied", "message": "the peer role does not grant this operation"})
	})
}

func (s *Server) recordAuthorization(_ *http.Request, input ledger.AuthorizationDecisionInput) {
	if s.Service != nil && s.Service.Ledger != nil {
		_ = s.Service.Ledger.RecordAuthorizationDecision(input)
	}
}
func (s *Server) authorizationStatus() any {
	if s.Authorizer == nil {
		return map[string]any{"enabled": false}
	}
	return map[string]any{"enabled": true, "policy_digest": s.Authorizer.Digest(), "capabilities_enabled": s.CapabilityKeyring != nil, "resource_isolation": true}
}
