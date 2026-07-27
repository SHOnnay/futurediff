package api

import (
	"github.com/SHOnnay/futurediff/internal/requestid"
	"net/http"
)

func (s *Server) requestIDGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := requestid.Normalize(r.Header.Get("X-Request-ID"))
		if !ok {
			id = requestid.New()
		}
		w.Header().Set("X-Request-ID", id)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r.WithContext(requestid.With(r.Context(), id)))
	})
}
