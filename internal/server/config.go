package server

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

// config renders the lazy-loaded Configuration tab: Options toggles, the
// limits/profiles editors, and the raw key/value editor. Devices live on their
// own tab (devicesPanel).
func (h handlers) config(w http.ResponseWriter, r *http.Request) {
	h.renderConfig(w, r, r.PathValue("name"), "")
}

// renderConfig gathers the Configuration tab's data and renders it, with an
// optional out-of-band success toast for the mutating callers.
func (h handlers) renderConfig(w http.ResponseWriter, r *http.Request, name, msg string) {
	inst, err := h.backend.GetInstance(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	caps := h.backend.Capabilities(r.Context())
	var profiles []backend.Profile
	if caps.Profiles {
		if profiles, err = h.backend.ListProfiles(r.Context()); err != nil {
			h.fail(w, r, err)
			return
		}
	}
	panel := ui.ConfigPanel(caps, inst, profiles, cfg)
	if msg == "" {
		h.render(w, r, http.StatusOK, panel)
		return
	}
	h.renderWithToast(w, r, http.StatusOK, panel, msg)
}

// updateOptions applies the friendly Options toggles: it merges just the
// ui.InstanceOptions keys into the current editable config (checked = "true",
// unchecked = key removed) so the raw editor's other keys are untouched. The
// version token makes the read-merge-write conditional, same as updateConfig.
func (h handlers) updateOptions(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if r.Form.Get("version") == "" {
		h.fail(w, r, fmt.Errorf("missing config version token: %w", backend.ErrInvalid))
		return
	}
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	next := maps.Clone(cfg.Config)
	if next == nil {
		next = map[string]string{}
	}
	for _, opt := range ui.InstanceOptions {
		if r.Form.Get(opt.Key) != "" {
			next[opt.Key] = "true"
		} else {
			delete(next, opt.Key)
		}
	}
	if err := h.backend.UpdateInstanceConfig(r.Context(), name, next, r.Form.Get("version")); err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		redirectToInstanceTab(w, name, "config")
		return
	}
	h.renderConfig(w, r, name, "Options saved")
}

// devicesPanel renders the lazy-loaded Devices tab: local devices (editable),
// inherited devices (read-only), and the typed add forms.
func (h handlers) devicesPanel(w http.ResponseWriter, r *http.Request) {
	h.renderDevices(w, r, r.PathValue("name"), "")
}

// updateConfig replaces the instance's editable config from the parallel
// key/value form fields (whole-map replace; blank keys are dropped), then
// re-renders the panel. The hidden version field makes the write conditional,
// so a concurrent change conflicts (409) instead of being silently overwritten
// (same contract as updateDevice).
func (h handlers) updateConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	// The editor always carries the token; a request without one would write
	// unconditionally, defeating the conflict protection.
	if r.Form.Get("version") == "" {
		h.fail(w, r, fmt.Errorf("missing config version token: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.UpdateInstanceConfig(r.Context(), name, zipConfigPairs(r.Form["key"], r.Form["value"]), r.Form.Get("version")); err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		redirectToInstanceTab(w, name, "config")
		return
	}
	h.renderConfig(w, r, name, "Configuration saved")
}

// addDevice attaches a local device built from the typed form (type + device
// name + that type's fields; blanks dropped), then re-renders the devices section.
func (h handlers) addDevice(w http.ResponseWriter, r *http.Request) {
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
	cfg := deviceConfigFromForm(r.Form.Get("type"), r.Form)
	if err := h.backend.AddDevice(r.Context(), name, device, cfg); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderDevices(w, r, name, "Device added")
}

// updateDevice applies the per-device edit form: the device type's known
// fields update in place (blank = remove that key); every other key —
// including "type" and keys the typed form doesn't know — is preserved. The
// hidden version field makes the write conditional, so a concurrent change
// conflicts (409) instead of being silently overwritten.
func (h handlers) updateDevice(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	device := r.PathValue("device")
	// The edit form always carries the token; a request without one would
	// write unconditionally, defeating the conflict protection.
	if r.Form.Get("version") == "" {
		h.fail(w, r, fmt.Errorf("missing config version token: %w", backend.ErrInvalid))
		return
	}
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	current, ok := cfg.LocalDevices[device]
	if !ok {
		h.fail(w, r, fmt.Errorf("device %q on %q: %w", device, name, backend.ErrNotFound))
		return
	}
	next := mergeDeviceFields(current, r.Form)
	if err := h.backend.UpdateDevice(r.Context(), name, device, next, r.Form.Get("version")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderDevices(w, r, name, "Device updated")
}

// removeDevice detaches a local device, then re-renders the devices section.
func (h handlers) removeDevice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RemoveDevice(r.Context(), name, r.PathValue("device")); err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderDevices(w, r, name, "Device removed")
}

// renderDevices re-renders the devices section. A non-empty msg emits an
// out-of-band success toast (mutations); the lazy GET passes "" for no toast.
func (h handlers) renderDevices(w http.ResponseWriter, r *http.Request, name, msg string) {
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if !isHTMX(r) {
		redirectToInstance(w, name)
		return
	}
	section := ui.DevicesSection(h.backend.Capabilities(r.Context()), name, cfg)
	if msg == "" {
		h.render(w, r, http.StatusOK, section)
		return
	}
	h.renderWithToast(w, r, http.StatusOK, section, msg)
}

// mergeDeviceFields applies a device-edit form onto the device's current config:
// the device type's known fields update in place (blank submitted field =
// remove that key); every other key — including "type" and keys the typed form
// doesn't know — is preserved. Shared by instance and profile device editors.
func mergeDeviceFields(current map[string]string, form url.Values) map[string]string {
	next := maps.Clone(current)
	for _, dt := range ui.DeviceTypes {
		if dt.Type != current["type"] {
			continue
		}
		for _, f := range dt.Fields {
			if !form.Has(f) {
				continue // only fields the form submitted
			}
			if v := strings.TrimSpace(form.Get(f)); v != "" {
				next[f] = v
			} else {
				delete(next, f)
			}
		}
	}
	return next
}

// deviceConfigFromForm builds a device config from the form's non-blank fields
// for the given type (per ui.DeviceTypes), always setting "type". Incus validates
// the values.
func deviceConfigFromForm(devType string, form url.Values) map[string]string {
	cfg := map[string]string{"type": devType}
	for _, dt := range ui.DeviceTypes {
		if dt.Type != devType {
			continue
		}
		for _, f := range dt.Fields {
			if v := strings.TrimSpace(form.Get(f)); v != "" {
				cfg[f] = v
			}
		}
	}
	return cfg
}

// zipConfigPairs pairs parallel key/value form fields into a map, dropping
// entries with a blank (trimmed) key. A missing value maps to "".
func zipConfigPairs(keys, values []string) map[string]string {
	out := make(map[string]string, len(keys))
	for i, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		v := ""
		if i < len(values) {
			// Textareas submit newlines as CRLF; config values are stored LF.
			v = strings.ReplaceAll(values[i], "\r\n", "\n")
		}
		out[k] = v
	}
	return out
}
