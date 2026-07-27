package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/SHOnnay/futurediff/internal/peerauth"
	"github.com/SHOnnay/futurediff/internal/requestid"
)

const maxRequestBodyBytes = 1 << 20

var idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]{8,128}$`)

type captureWriter struct {
	header      http.Header
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func newCaptureWriter() *captureWriter {
	return &captureWriter{header: make(http.Header), status: http.StatusOK}
}
func (w *captureWriter) Header() http.Header { return w.header }
func (w *captureWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
}
func (w *captureWriter) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	if w.body.Len()+len(p) > 1<<20 {
		return 0, errors.New("response exceeds 1 MiB idempotency limit")
	}
	return w.body.Write(p)
}
func (w *captureWriter) flush(dst http.ResponseWriter) {
	for k, values := range w.header {
		for _, v := range values {
			dst.Header().Add(k, v)
		}
	}
	dst.WriteHeader(w.status)
	_, _ = dst.Write(w.body.Bytes())
}

func (s *Server) idempotencyGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions || s.Service == nil || s.Service.Ledger == nil {
			next.ServeHTTP(w, r)
			return
		}
		body, err := readLimitedBody(r.Body)
		if err != nil {
			principal := peerauth.Principal(r.Context())
			_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusRequestEntityTooLarge, "", "", requestid.From(r.Context()))
			writeErr(w, http.StatusRequestEntityTooLarge, "request_too_large", err)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		principal := peerauth.Principal(r.Context())
		requestDigest := requestSHA256(r.Method, r.URL.Path, body)
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		keyDigest := ""
		if key != "" {
			keyDigest = digestText(key)
			if !idempotencyKeyPattern.MatchString(key) {
				_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusBadRequest, keyDigest, requestDigest, requestid.From(r.Context()))
				writeErr(w, http.StatusBadRequest, "invalid_idempotency_key", errors.New("Idempotency-Key must be 8-128 characters using letters, digits, dot, underscore, colon or dash"))
				return
			}
		}

		if key == "" {
			cw := newCaptureWriter()
			next.ServeHTTP(cw, r)
			_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, cw.status, "", requestDigest, requestid.From(r.Context()))
			cw.flush(w)
			return
		}

		s.idempotencyMu.Lock()
		defer s.idempotencyMu.Unlock()
		record, created, err := s.Service.Ledger.BeginAPIRequest(principal, key, r.Method, r.URL.Path, requestDigest)
		if err != nil {
			_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusInternalServerError, keyDigest, requestDigest, requestid.From(r.Context()))
			writeErr(w, http.StatusInternalServerError, "idempotency_failed", err)
			return
		}
		if !created {
			if record.Method != r.Method || record.Path != r.URL.Path || record.RequestDigest != requestDigest {
				_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusConflict, keyDigest, requestDigest, requestid.From(r.Context()))
				writeErr(w, http.StatusConflict, "idempotency_conflict", errors.New("Idempotency-Key was already used for a different request"))
				return
			}
			if record.State == "completed" {
				if record.ResponseContentType != "" {
					w.Header().Set("Content-Type", record.ResponseContentType)
				}
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(record.StatusCode)
				_, _ = w.Write(record.ResponseBody)
				_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, record.StatusCode, keyDigest, requestDigest, requestid.From(r.Context()))
				return
			}
			_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusTooEarly, keyDigest, requestDigest, requestid.From(r.Context()))
			writeErr(w, http.StatusTooEarly, "idempotency_in_progress", errors.New("an identical request is still in progress"))
			return
		}

		cw := newCaptureWriter()
		next.ServeHTTP(cw, r)
		contentType := cw.header.Get("Content-Type")
		if cw.status >= 500 {
			_ = s.Service.Ledger.AbortAPIRequest(principal, key, requestDigest)
		} else if err := s.Service.Ledger.CompleteAPIRequest(principal, key, requestDigest, cw.status, contentType, cw.body.Bytes()); err != nil {
			_ = s.Service.Ledger.AbortAPIRequest(principal, key, requestDigest)
			_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, http.StatusInternalServerError, keyDigest, requestDigest, requestid.From(r.Context()))
			writeErr(w, http.StatusInternalServerError, "idempotency_persist_failed", err)
			return
		}
		_ = s.Service.Ledger.RecordAPIAccess(principal, r.Method, r.URL.Path, cw.status, keyDigest, requestDigest, requestid.From(r.Context()))
		cw.flush(w)
	})
}

func readLimitedBody(body io.ReadCloser) ([]byte, error) {
	if body == nil {
		return nil, nil
	}
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, maxRequestBodyBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxRequestBodyBytes {
		return nil, errors.New("request body exceeds 1 MiB")
	}
	return data, nil
}

func requestSHA256(method, path string, body []byte) string {
	h := sha256.New()
	_, _ = h.Write([]byte(method))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(path))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write(body)
	return hex.EncodeToString(h.Sum(nil))
}
func digestText(v string) string { sum := sha256.Sum256([]byte(v)); return hex.EncodeToString(sum[:]) }
