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

func (h handlers) networkACLs(w http.ResponseWriter, r *http.Request) {
	acls, err := h.backend.ListNetworkACLs(r.Context())
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkACLsPage(h.backend.Capabilities(r.Context()), acls))
}

func (h handlers) networkACLDetail(w http.ResponseWriter, r *http.Request) {
	acl, err := h.backend.GetNetworkACL(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, r, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.NetworkACLDetailPage(h.backend.Capabilities(r.Context()), acl))
}

// createNetworkACL makes an empty ACL (name + description) and redirects to
// its detail page, where rules are added.
func (h handlers) createNetworkACL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, r, fmt.Errorf("ACL name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.CreateNetworkACL(r.Context(), backend.NetworkACL{Name: name, Description: r.Form.Get("description")}); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls/"+url.PathEscape(name), http.StatusSeeOther)
}

// updateNetworkACL applies the description editor, preserving the current
// rules. The hidden version field makes the write conditional (409 on a
// concurrent change).
func (h handlers) updateNetworkACL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	acl, err := h.backend.GetNetworkACL(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	if err := h.backend.UpdateNetworkACL(r.Context(), name, r.Form.Get("description"), acl.Ingress, acl.Egress, backend.Version(r.Form.Get("version"))); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls/"+url.PathEscape(name), http.StatusSeeOther)
}

func (h handlers) renameNetworkACL(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, r, fmt.Errorf("new ACL name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameNetworkACL(r.Context(), r.PathValue("name"), newName); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls/"+url.PathEscape(newName), http.StatusSeeOther)
}

func (h handlers) deleteNetworkACL(w http.ResponseWriter, r *http.Request) {
	if err := h.backend.DeleteNetworkACL(r.Context(), r.PathValue("name")); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls", http.StatusSeeOther)
}

// addNetworkACLRule appends a rule built from the typed form to the chosen
// direction's list (the version token carries the optimistic lock).
func (h handlers) addNetworkACLRule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	direction := r.Form.Get("direction")
	if direction != "ingress" && direction != "egress" {
		h.fail(w, r, fmt.Errorf("direction %q must be ingress or egress: %w", direction, backend.ErrInvalid))
		return
	}
	// The rule forms always carry the token; a request without one would write
	// unconditionally, letting a stale form clobber concurrent rule changes.
	version, ok := h.requireVersion(w, r, "ACL")
	if !ok {
		return
	}
	rule := backend.NetworkACLRule{
		Action:          backend.ACLRuleAction(r.Form.Get("action")),
		Source:          strings.TrimSpace(r.Form.Get("source")),
		Destination:     strings.TrimSpace(r.Form.Get("destination")),
		Protocol:        backend.ACLProtocol(r.Form.Get("protocol")),
		SourcePort:      strings.TrimSpace(r.Form.Get("source_port")),
		DestinationPort: strings.TrimSpace(r.Form.Get("destination_port")),
		State:           backend.ACLRuleState(r.Form.Get("state")),
	}
	acl, err := h.backend.GetNetworkACL(r.Context(), name)
	if err != nil {
		h.fail(w, r, err)
		return
	}
	ingress, egress := acl.Ingress, acl.Egress
	if direction == "ingress" {
		ingress = append(ingress, rule)
	} else {
		egress = append(egress, rule)
	}
	if err := h.backend.UpdateNetworkACL(r.Context(), name, acl.Description, ingress, egress, version); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls/"+url.PathEscape(name), http.StatusSeeOther)
}

// deleteNetworkACLRule removes the rule at the submitted direction+index (the
// rule lists are order-stable between the render and the conditional update,
// enforced by the version token).
func (h handlers) deleteNetworkACLRule(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	direction := r.Form.Get("direction")
	// Index-based removal is only safe under the optimistic lock: without the
	// token a concurrent rule change could shift indices between render and
	// write.
	version, ok := h.requireVersion(w, r, "ACL")
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.Form.Get("index"))
	if err != nil {
		h.fail(w, r, fmt.Errorf("rule index %q: %w", r.Form.Get("index"), backend.ErrInvalid))
		return
	}
	acl, getErr := h.backend.GetNetworkACL(r.Context(), name)
	if getErr != nil {
		h.fail(w, r, getErr)
		return
	}
	ingress, egress := acl.Ingress, acl.Egress
	switch direction {
	case "ingress":
		if index < 0 || index >= len(ingress) {
			h.fail(w, r, fmt.Errorf("ingress rule %d out of range: %w", index, backend.ErrInvalid))
			return
		}
		ingress = append(ingress[:index], ingress[index+1:]...)
	case "egress":
		if index < 0 || index >= len(egress) {
			h.fail(w, r, fmt.Errorf("egress rule %d out of range: %w", index, backend.ErrInvalid))
			return
		}
		egress = append(egress[:index], egress[index+1:]...)
	default:
		h.fail(w, r, fmt.Errorf("direction %q must be ingress or egress: %w", direction, backend.ErrInvalid))
		return
	}
	if err := h.backend.UpdateNetworkACL(r.Context(), name, acl.Description, ingress, egress, version); err != nil {
		h.fail(w, r, err)
		return
	}
	http.Redirect(w, r, "/network-acls/"+url.PathEscape(name), http.StatusSeeOther)
}
