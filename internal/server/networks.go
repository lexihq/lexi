package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/ui"
)

func (h handlers) networks(w http.ResponseWriter, r *http.Request) {
	nets, err := h.backend.ListNetworks(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworksPage(h.backend.Capabilities(r.Context()), nets))
}

func (h handlers) networkDetail(w http.ResponseWriter, r *http.Request) {
	n, err := h.backend.GetNetwork(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	caps := h.backend.Capabilities(r.Context())
	// State, leases, and forwards exist for managed networks only; the page
	// hides those sections otherwise, so zero values suffice.
	var st backend.NetworkState
	var leases []backend.NetworkLease
	var forwards []backend.NetworkForward
	if n.Managed {
		if st, err = h.backend.GetNetworkState(r.Context(), n.Name); err != nil {
			h.fail(w, err)
			return
		}
		if leases, err = h.backend.ListNetworkLeases(r.Context(), n.Name); err != nil {
			h.fail(w, err)
			return
		}
		if caps.NetworkForwards {
			if forwards, err = h.backend.ListNetworkForwards(r.Context(), n.Name); err != nil {
				h.fail(w, err)
				return
			}
		}
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkDetailPage(caps, n, st, leases, forwards))
}

// createNetworkForward adds a port forward to a managed network.
func (h handlers) createNetworkForward(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	network := r.PathValue("name")
	fw := backend.NetworkForward{
		ListenAddress: strings.TrimSpace(r.Form.Get("listen_address")),
		Description:   strings.TrimSpace(r.Form.Get("description")),
		DefaultTarget: strings.TrimSpace(r.Form.Get("target_address")),
	}
	if fw.ListenAddress == "" {
		h.fail(w, fmt.Errorf("listen address is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateNetworkForward(r.Context(), network, fw); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks/"+url.PathEscape(network), http.StatusSeeOther)
}

// updateNetworkForward replaces a forward's description, default target, and
// port set from the per-forward editor (rows with an empty listen port are
// dropped, so clearing one removes the mapping).
func (h handlers) updateNetworkForward(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	network, addr := r.PathValue("name"), r.PathValue("addr")
	fw := backend.NetworkForward{
		ListenAddress: addr,
		Description:   strings.TrimSpace(r.Form.Get("description")),
		DefaultTarget: strings.TrimSpace(r.Form.Get("target_address")),
	}
	protos, listens := r.Form["port_protocol"], r.Form["listen_port"]
	targets, tports := r.Form["port_target"], r.Form["target_port"]
	for i, lp := range listens {
		if strings.TrimSpace(lp) == "" {
			continue
		}
		p := backend.ForwardPort{ListenPort: strings.TrimSpace(lp)}
		if i < len(protos) {
			p.Protocol = protos[i]
		}
		if i < len(targets) {
			p.TargetAddress = strings.TrimSpace(targets[i])
		}
		if i < len(tports) {
			p.TargetPort = strings.TrimSpace(tports[i])
		}
		fw.Ports = append(fw.Ports, p)
	}
	if err := h.backend.UpdateNetworkForward(r.Context(), network, fw); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks/"+url.PathEscape(network), http.StatusSeeOther)
}

func (h handlers) deleteNetworkForward(w http.ResponseWriter, r *http.Request) {
	network := r.PathValue("name")
	if err := h.backend.DeleteNetworkForward(r.Context(), network, r.PathValue("addr")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks/"+url.PathEscape(network), http.StatusSeeOther)
}

func (h handlers) networkCreateForm(w http.ResponseWriter, r *http.Request) {
	h.renderShell(w, r, http.StatusOK, ui.NetworkCreatePage(h.backend.Capabilities(r.Context())))
}

// createNetwork builds a network from the form (name/type/description + optional
// key/value config rows) and redirects to the list. Incus validates the config.
func (h handlers) createNetwork(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	netType := strings.TrimSpace(r.Form.Get("type"))
	if name == "" || netType == "" {
		h.fail(w, fmt.Errorf("network name and type are required: %w", backend.ErrInvalid))
		return
	}
	n := backend.Network{
		Name:        name,
		Type:        netType,
		Description: r.Form.Get("description"),
		Config:      zipConfigPairs(r.Form["key"], r.Form["value"]),
	}
	if err := h.backend.CreateNetwork(r.Context(), n); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks", http.StatusSeeOther)
}

// updateNetwork applies the network editor: description plus key/value rows
// that replace the network's config (instance-config-editor semantics). The
// hidden version field carries the token the form was rendered from, so a
// concurrent change conflicts (409) instead of being silently overwritten.
func (h handlers) updateNetwork(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateNetwork(r.Context(), name, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/networks/"+url.PathEscape(name), http.StatusSeeOther)
}

// deleteNetwork removes a network, then re-renders the list table on HTMX.
func (h handlers) deleteNetwork(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteNetwork(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, err)
		return
	}
	nets, err := h.backend.ListNetworks(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	if isHTMX(r) {
		h.render(w, r, http.StatusOK, ui.NetworksTable(nets))
		return
	}
	http.Redirect(w, r, "/networks", http.StatusSeeOther)
}
