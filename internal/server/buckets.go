package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
)

// createBucket makes a bucket (name + optional description/size) and
// redirects back to the pool page.
func (h handlers) createBucket(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, fmt.Errorf("bucket name is required: %w", backend.ErrInvalid))
		return
	}
	size := strings.TrimSpace(r.Form.Get("size"))
	if err := h.backend.CreateBucket(r.Context(), pool, name, r.Form.Get("description"), size); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool), http.StatusSeeOther)
}

func (h handlers) deleteBucket(w http.ResponseWriter, r *http.Request) {
	pool := r.PathValue("pool")
	if err := h.backend.DeleteBucket(r.Context(), pool, r.PathValue("bucket")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool), http.StatusSeeOther)
}

// createBucketKey adds a credential; the generated keys appear in the
// bucket's key table after the redirect.
func (h handlers) createBucketKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, fmt.Errorf("key name is required: %w", backend.ErrInvalid))
		return
	}
	role := r.Form.Get("role")
	if role != "" && role != "admin" && role != "read-only" {
		h.fail(w, fmt.Errorf("key role %q must be admin or read-only: %w", role, backend.ErrInvalid))
		return
	}
	if _, err := h.backend.CreateBucketKey(r.Context(), pool, r.PathValue("bucket"), name, r.Form.Get("description"), role); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool), http.StatusSeeOther)
}

// deleteBucketKey revokes the form-submitted key.
func (h handlers) deleteBucketKey(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	pool := r.PathValue("pool")
	key := r.Form.Get("key")
	if key == "" {
		h.fail(w, fmt.Errorf("key name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.DeleteBucketKey(r.Context(), pool, r.PathValue("bucket"), key); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/storage/"+url.PathEscape(pool), http.StatusSeeOther)
}
