package middleware

import (
	"net/http"

	"github.com/doki-stack/shared-go/envelope"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// OrgID extracts and validates org_id, storing it in context.
// Extraction order: (1) X-Org-Id header (primary, set by Kong from JWT), (2) chi URL param :org_id (fallback).
// Rejects with 400 if missing or invalid UUID.
func OrgID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		orgID := r.Header.Get("X-Org-Id")
		if orgID == "" {
			orgID = chi.URLParam(r, "org_id")
		}
		if orgID == "" {
			envelope.WriteJSON(w, http.StatusBadRequest, envelope.New(envelope.BadRequest, "missing or invalid org_id"))
			return
		}
		if _, err := uuid.Parse(orgID); err != nil {
			envelope.WriteJSON(w, http.StatusBadRequest, envelope.New(envelope.BadRequest, "missing or invalid org_id"))
			return
		}
		ctx := ContextWithOrgID(r.Context(), orgID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
