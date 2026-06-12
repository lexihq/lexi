package server

import (
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"regexp"
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
	caps := h.backend.Capabilities(r.Context())
	// instancesUsage backs the Resources column: instances per project from
	// the state API, replacing the misleading UsedBy count.
	var instancesUsage map[string]int64
	if caps.ProjectUsage {
		instancesUsage = make(map[string]int64, len(projects))
		for _, p := range projects {
			usage, err := h.backend.GetProjectUsage(r.Context(), p.Name)
			if err != nil {
				h.fail(w, err)
				return
			}
			for _, u := range usage {
				if u.Resource == "instances" {
					instancesUsage[p.Name] = u.Usage
				}
			}
		}
	}
	h.renderShell(w, r, http.StatusOK, ui.ProjectsPage(caps, projects, backend.ProjectFromContext(r.Context()), instancesUsage))
}

func (h handlers) projectDetail(w http.ResponseWriter, r *http.Request) {
	p, err := h.backend.GetProject(r.Context(), r.PathValue("name"))
	if err != nil {
		h.fail(w, err)
		return
	}
	caps := h.backend.Capabilities(r.Context())
	var usage []backend.ProjectUsage
	if caps.ProjectUsage {
		if usage, err = h.backend.GetProjectUsage(r.Context(), p.Name); err != nil {
			h.fail(w, err)
			return
		}
	}
	h.renderShell(w, r, http.StatusOK, ui.ProjectDetailPage(caps, p, usage))
}

// projectLimitFields maps the limits form to project config keys; Size
// selects size-string validation over non-negative integers.
var projectLimitFields = []struct {
	Field string
	Key   string
	Size  bool
}{
	{"instances", "limits.instances", false},
	{"containers", "limits.containers", false},
	{"virtual_machines", "limits.virtual-machines", false},
	{"cpu", "limits.cpu", false},
	{"memory", "limits.memory", true},
	{"disk", "limits.disk", true},
}

// sizeValue is the daemon's byte-size notation: an integer with an optional
// decimal or binary unit suffix.
var sizeValue = regexp.MustCompile(`^[0-9]+\s?(B|kB|MB|GB|TB|PB|EB|KiB|MiB|GiB|TiB|PiB|EiB)?$`)

// updateProjectLimits applies the validated limits form onto the project
// config via read-modify-write under the read's version token. An empty
// field removes its key; all fields are validated before anything applies.
func (h handlers) updateProjectLimits(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	name := r.PathValue("name")
	p, err := h.backend.GetProject(r.Context(), name)
	if err != nil {
		h.fail(w, err)
		return
	}
	config := maps.Clone(p.Config)
	if config == nil {
		config = map[string]string{}
	}
	for _, f := range projectLimitFields {
		raw := strings.TrimSpace(r.Form.Get(f.Field))
		if raw == "" {
			delete(config, f.Key)
			continue
		}
		if f.Size {
			if !sizeValue.MatchString(raw) {
				h.fail(w, fmt.Errorf("%s must be a size like 1GiB: %w", f.Key, backend.ErrInvalid))
				return
			}
		} else if n, err := strconv.Atoi(raw); err != nil || n < 0 {
			h.fail(w, fmt.Errorf("%s must be a non-negative integer: %w", f.Key, backend.ErrInvalid))
			return
		}
		config[f.Key] = raw
	}
	if err := h.backend.UpdateProject(r.Context(), name, p.Description, config, p.Version); err != nil {
		h.fail(w, err)
		return
	}
	http.Redirect(w, r, "/projects/"+url.PathEscape(name), http.StatusSeeOther)
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
