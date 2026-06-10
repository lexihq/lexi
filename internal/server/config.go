package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

// config renders the lazy-loaded Configuration tab: the key/value editor over
// the instance's editable config. Devices live on their own tab (devicesPanel).
func (h handlers) config(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	h.render(w, r, http.StatusOK, ui.ConfigPanel(name, cfg))
}

// devicesPanel renders the lazy-loaded Devices tab: local devices (editable),
// inherited devices (read-only), and the typed add forms.
func (h handlers) devicesPanel(w http.ResponseWriter, r *http.Request) {
	h.renderDevices(w, r, r.PathValue("name"))
}

// updateConfig replaces the instance's editable config from the parallel
// key/value form fields (whole-map replace; blank keys are dropped), then
// re-renders the panel.
func (h handlers) updateConfig(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	if err := h.backend.UpdateInstanceConfig(r.Context(), name, zipConfigPairs(r.Form["key"], r.Form["value"])); err != nil {
		h.fail(w, err)
		return
	}
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.ConfigPanel(name, cfg))
		return
	}
	redirectToInstance(w, name)
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
		h.fail(w, fmt.Errorf("device name required: %w", backend.ErrInvalid))
		return
	}
	cfg := deviceConfigFromForm(r.Form.Get("type"), r.Form)
	if err := h.backend.AddDevice(r.Context(), name, device, cfg); err != nil {
		h.fail(w, err)
		return
	}
	h.renderDevices(w, r, name)
}

// removeDevice detaches a local device, then re-renders the devices section.
func (h handlers) removeDevice(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.RemoveDevice(r.Context(), name, r.PathValue("device")); err != nil {
		h.fail(w, err)
		return
	}
	h.renderDevices(w, r, name)
}

func (h handlers) renderDevices(w http.ResponseWriter, r *http.Request, name string) {
	cfg, err := h.backend.GetInstanceConfig(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.DevicesSection(h.backend.Capabilities(), name, cfg))
		return
	}
	redirectToInstance(w, name)
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
