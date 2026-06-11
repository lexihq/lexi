package server

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/ui"
)

func (h handlers) projectsPage(w http.ResponseWriter, r *http.Request) {
	projects, err := h.backend.ListProjects(r.Context())
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProjectsPage(h.backend.Capabilities(r.Context()), projects, backend.ProjectFromContext(r.Context())))
}

func (h handlers) projectDetail(w http.ResponseWriter, r *http.Request) {
	p, err := h.backend.GetProject(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	h.renderShell(w, r, http.StatusOK, ui.ProjectDetailPage(h.backend.Capabilities(r.Context()), p))
}

// createProject makes a project from the name/description/feature checkboxes.
// Unchecked features are sent as explicit "false" — omitted default-enabled
// features would be injected as "true" by the daemon.
func (h handlers) createProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.Form.Get("name"))
	if name == "" {
		h.fail(w, fmt.Errorf("project name is required: %w", backend.ErrInvalid))
		return
	}
	config := map[string]string{}
	for _, feature := range []string{"features.images", "features.profiles", "features.storage.volumes", "features.networks"} {
		config[feature] = strconv.FormatBool(r.Form.Get(feature) != "")
	}
	if err := h.backend.CreateProject(r.Context(), name, r.Form.Get("description"), config); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(name), http.StatusSeeOther)
}

// updateProject applies the config editor: description plus key/value rows
// replacing the project's config under the hidden version token.
func (h handlers) updateProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	config := zipConfigPairs(r.Form["key"], r.Form["value"])
	if err := h.backend.UpdateProject(r.Context(), name, r.Form.Get("description"), config, r.Form.Get("version")); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(name), http.StatusSeeOther)
}

// renameProject renames a project; renaming the currently-selected one
// rewrites the selection cookie so the session follows it.
func (h handlers) renameProject(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	newName := strings.TrimSpace(r.Form.Get("new_name"))
	if newName == "" {
		h.fail(w, fmt.Errorf("new project name is required: %w", backend.ErrInvalid))
		return
	}
	if err := h.backend.RenameProject(r.Context(), name, newName); err != nil {
		h.fail(w, err)
		return
	}
	if backend.ProjectFromContext(r.Context()) == name {
		setProjectCookie(w, newName)
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(newName), http.StatusSeeOther)
}

// deleteProject removes an empty project; deleting the currently-selected
// one clears the selection cookie so the session falls back to default.
func (h handlers) deleteProject(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if err := h.backend.DeleteProject(r.Context(), name); err != nil {
		h.fail(w, err)
		return
	}
	if backend.ProjectFromContext(r.Context()) == name {
		expireProjectCookie(w)
	}
	http.Redirect(w, r, "/projects", http.StatusSeeOther)
}
