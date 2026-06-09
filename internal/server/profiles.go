package server

import (
	"net/http"

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
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	ordered := mergeProfileOrder(inst.Profiles, r.Form["profile"])
	if err := h.backend.SetInstanceProfiles(r.Context(), name, ordered); err != nil {
		h.renderError(w, statusFor(err), err.Error())
		return
	}
	all, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		h.renderError(w, statusFor(err), err.Error())
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
