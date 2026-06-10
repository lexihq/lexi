package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) profiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfilesPage(h.backend.Capabilities(), profiles))
}

func (h handlers) profileDetail(w http.ResponseWriter, r *http.Request) {
	p, err := h.backend.GetProfile(r.Context(), r.PathValue("name"))
	if err != nil {
		http.Error(w, err.Error(), statusFor(err))
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfileDetailPage(h.backend.Capabilities(), p))
}

// createProfile makes an empty profile (name + description) and redirects to
// its detail page, where config can be edited.
func (h handlers) createProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, fmt.Errorf("profile name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateProfile(r.Context(), name, r.Form.Get("description")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/profiles/"+url.PathEscape(name), http.StatusSeeOther)
}

// updateProfile applies the profile editor: description plus key/value rows
// that replace the profile's config (devices are preserved by the backend).
// The hidden version field carries the token the form was rendered from, so a
// concurrent change conflicts (409) instead of being silently overwritten.
func (h handlers) updateProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateProfile(r.Context(), name, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/profiles/"+url.PathEscape(name), http.StatusSeeOther)
}

// deleteProfile removes a profile and redirects to the list ("default" and
// in-use profiles are refused by the backend).
func (h handlers) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteProfile(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

// setInstanceProfiles replaces the instance's profile set from the checked
// boxes, preserving existing order and appending additions (mergeProfileOrder),
// then returns the updated control on HTMX.
func (h handlers) setInstanceProfiles(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	ordered := mergeProfileOrder(inst.Profiles, r.Form["profile"])
	if err := h.backend.SetInstanceProfiles(r.Context(), name, ordered); err != nil {
		h.fail(w, err)
		return
	}
	all, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	inst.Profiles = ordered
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.InstanceProfilesForm(inst, all))
		return
	}
	redirectToInstance(w, name)
}

// mergeProfileOrder keeps currently-assigned profiles that are still checked in
// their existing order, then appends newly-checked profiles in checked order. It
// dedupes so a doubled checkbox cannot duplicate an entry.
func mergeProfileOrder(current, checked []string) []string {
	inChecked := make(map[string]bool, len(checked))
	for _, c := range checked {
		inChecked[c] = true
	}
	out := make([]string, 0, len(checked))
	seen := make(map[string]bool, len(checked))
	for _, c := range current {
		if inChecked[c] && !seen[c] {
			out = append(out, c)
			seen[c] = true
		}
	}
	for _, c := range checked {
		if !seen[c] {
			out = append(out, c)
			seen[c] = true
		}
	}
	return out
}
