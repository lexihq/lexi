package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

func (h handlers) networkZones(w http.ResponseWriter, r *http.Request) {
	zones, err := h.backend.ListNetworkZones(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkZonesPage(h.backend.Capabilities(r.Context()), zones))
}

func (h handlers) networkZoneDetail(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	zone, err := h.backend.GetNetworkZone(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	records, err := h.backend.ListZoneRecords(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkZoneDetailPage(h.backend.Capabilities(r.Context()), zone, records))
}

// createNetworkZone makes a zone (name + description) and redirects to its
// detail page, where records are added.
func (h handlers) createNetworkZone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, r, fmt.Errorf("zone name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateNetworkZone(r.Context(), backend.NetworkZone{Name: name, Description: r.Form.Get("description")}); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-zones/"+url.PathEscape(name), http.StatusSeeOther)
}

// updateNetworkZone applies the description/config editor under the hidden
// version token (409 on a concurrent change).
func (h handlers) updateNetworkZone(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateNetworkZone(r.Context(), name, r.Form.Get("description"), config, backend.Version(r.Form.Get("version"))); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-zones/"+url.PathEscape(name), http.StatusSeeOther)
}

func (h handlers) deleteNetworkZone(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteNetworkZone(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-zones", http.StatusSeeOther)
}

// addZoneRecord creates a record set with a single entry from the typed form.
func (h handlers) addZoneRecord(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zone := r.PathValue("name")
	record := strings.TrimSpace(r.Form.Get("record"))
	entryType := backend.ZoneEntryType(r.Form.Get("type"))
	value := strings.TrimSpace(r.Form.Get("value"))
	if record == "" || entryType == "" || value == "" {
		h.fail(w, r, fmt.Errorf("record name, type, and value are required: %w", backend.ErrInvalid))
		return
	}
	var ttl uint64
	if raw := strings.TrimSpace(r.Form.Get("ttl")); raw != "" {
		parsed, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			h.fail(w, r, fmt.Errorf("TTL %q must be a number of seconds: %w", raw, backend.ErrInvalid))
			return
		}
		ttl = parsed
	}
	rec := backend.ZoneRecord{
		Name:        record,
		Description: r.Form.Get("description"),
		Entries:     []backend.ZoneEntry{{Type: entryType, TTL: ttl, Value: value}},
	}
	if err := h.backend.CreateZoneRecord(r.Context(), zone, rec); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-zones/"+url.PathEscape(zone), http.StatusSeeOther)
}

// deleteZoneRecord removes a record set by its form-submitted name (record
// names contain dots, so they stay out of the path).
func (h handlers) deleteZoneRecord(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	zone := r.PathValue("name")
	record := r.Form.Get("record")
	if record == "" {
		h.fail(w, r, fmt.Errorf("record name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.DeleteZoneRecord(r.Context(), zone, record); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-zones/"+url.PathEscape(zone), http.StatusSeeOther)
}
