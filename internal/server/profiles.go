package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

func (h handlers) profiles(w http.ResponseWriter, r *http.Request) {
	profiles, err := h.backend.ListProfiles(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfilesPage(h.backend.Capabilities(r.Context()), profiles))
}

func (h handlers) profileDetail(w http.ResponseWriter, r *http.Request) {
	p, err := h.backend.GetProfile(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProfileDetailPage(h.backend.Capabilities(r.Context()), p))
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
		h.fail(w, r, fmt.Errorf("profile name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateProfile(r.Context(), name, r.Form.Get("description")); err != nil {
		h.fail(w, r, err)
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
	if err := h.backend.UpdateProfile(r.Context(), name, r.Form.Get("description"), config, backend.Version(r.Form.Get("version"))); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/profiles/"+url.PathEscape(name), http.StatusSeeOther)
}

// deleteProfile removes a profile and redirects to the list ("default" and
// in-use profiles are refused by the backend).
func (h handlers) deleteProfile(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteProfile(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/profiles", http.StatusSeeOther)
}

// renameProfile renames a profile and redirects to its new detail page
// ("default" and name collisions are refused by the backend).
func (h handlers) renameProfile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, r, fmt.Errorf("new profile name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameProfile(r.Context(), r.PathValue("name"), newName); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/profiles/"+url.PathEscape(newName), http.StatusSeeOther)
}

// addProfileDevice attaches a device built from the typed form, then re-renders
// the profile's devices section.
func (h handlers) addProfileDevice(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	device := strings.TrimSpace(r.Form.Get("device"))
	if device == "" {
		h.fail(w, r, fmt.Errorf("device name required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.AddProfileDevice(r.Context(), name, device, deviceConfigFromForm(r.Form.Get("type"), r.Form)); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderProfileDevices(w, r, name)
}

// updateProfileDevice applies the per-device edit form (type's known fields,
// blank = remove; other keys preserved). The hidden version makes the write
// conditional on the profile state the panel was rendered from.
func (h handlers) updateProfileDevice(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	device := r.PathValue("device")
	version, ok := h.requireVersion(w, r, "profile")
	if !ok {
		return
	}
	p, err := h.backend.GetProfile(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	current, ok := p.Devices[device]
	if !ok {
		h.fail(w, r, fmt.Errorf("device %q on profile %q: %w", device, name, backend.ErrNotFound))
		return
	}
	if err := h.backend.UpdateProfileDevice(r.Context(), name, device, mergeDeviceFields(current, r.Form), version); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderProfileDevices(w, r, name)
}

// removeProfileDevice detaches a device, then re-renders the devices section.
func (h handlers) removeProfileDevice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RemoveProfileDevice(r.Context(), name, r.PathValue("device")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderProfileDevices(w, r, name)
}

func (h handlers) renderProfileDevices(w http.ResponseWriter, r *http.Request, name string) {
	p, err := h.backend.GetProfile(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.ProfileDevicesSection(h.backend.Capabilities(r.Context()), p))
		return
	}
	http.Redirect(w, r, "/profiles/"+url.PathEscape(name), http.StatusSeeOther)
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
		h.fail(w, r, err)
		return
	}
	ordered := mergeProfileOrder(inst.Profiles, r.Form["profile"])
	if err := h.backend.SetInstanceProfiles(r.Context(), name, ordered); err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		redirectToInstanceTab(w, name, "config")
		return
	}
	// Re-render the whole Configuration panel: applying profiles changes the
	// instance's config version, so the sibling Options/raw forms' hidden
	// tokens would otherwise go stale and 409 on their next save.
	h.renderConfig(w, r, name, "Profiles applied", false)
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
