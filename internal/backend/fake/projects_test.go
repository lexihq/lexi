package fake

import (
	"maps"
	"sort"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectCRUDRoundTrip(t *testing.T) {
	f := New()

	// The default project is seeded, like the daemon's.
	projects, err := f.ListProjects(ctx())
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "default", projects[0].Name)

	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "dev", Description: "dev project", Config: map[string]string{"features.profiles": "true"}}))
	require.ErrorIs(t, f.CreateProject(ctx(), backend.Project{Name: "dev", Description: ""}), backend.ErrConflict)
	require.ErrorIs(t, f.CreateProject(ctx(), backend.Project{Name: "bad name", Description: ""}), backend.ErrInvalid)

	p, err := f.GetProject(ctx(), "dev")
	require.NoError(t, err)
	require.NotEmpty(t, p.Version)
	assert.Equal(t, "true", p.Config["features.profiles"])
	assert.Equal(t, "dev project", p.Description)
	// Daemon parity: omitted default-enabled features are injected at create;
	// networks stays absent (= shared from default).
	assert.Equal(t, "true", p.Config["features.images"])
	assert.Equal(t, "true", p.Config["features.storage.volumes"])
	assert.NotContains(t, p.Config, "features.networks")

	// Update replaces description + config, conditionally on the version.
	require.NoError(t, f.UpdateProject(ctx(), "dev", "edited", map[string]string{"features.images": "false"}, p.Version))
	require.ErrorIs(t, f.UpdateProject(ctx(), "dev", "stale", nil, p.Version), backend.ErrConflict)
	got, err := f.GetProject(ctx(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "edited", got.Description)
	assert.Equal(t, "false", got.Config["features.images"])
	assert.NotContains(t, got.Config, "features.profiles", "update replaces the whole config")

	// The default project can be neither renamed nor deleted, like the daemon.
	require.ErrorIs(t, f.RenameProject(ctx(), "default", "x"), backend.ErrInvalid)
	require.ErrorIs(t, f.DeleteProject(ctx(), "default"), backend.ErrInvalid)

	// Rename moves the project; collisions and bad names are rejected.
	require.NoError(t, f.RenameProject(ctx(), "dev", "dev2"))
	_, err = f.GetProject(ctx(), "dev")
	require.ErrorIs(t, err, backend.ErrNotFound)
	require.ErrorIs(t, f.RenameProject(ctx(), "dev2", "default"), backend.ErrConflict)
	require.ErrorIs(t, f.RenameProject(ctx(), "dev2", "bad name"), backend.ErrInvalid)

	require.NoError(t, f.DeleteProject(ctx(), "dev2"))
	require.ErrorIs(t, f.DeleteProject(ctx(), "dev2"), backend.ErrNotFound)
	require.ErrorIs(t, f.RenameProject(ctx(), "ghost", "x"), backend.ErrNotFound)
}

// The scoping contract on the fake: a ctx-tagged project namespaces its
// instances/profiles/images/volumes; networks follow features.networks
// (shared from default unless the project owns them).
func TestProjectScopingIsolatesResources(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "c-default", Image: "alpine/edge"}))
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "dev", Description: ""}))
	dev := backend.WithProject(ctx(), "dev")

	// Instances are isolated; the new project starts empty and owns its
	// (empty) default profile.
	got, err := f.ListInstances(dev)
	require.NoError(t, err)
	assert.Empty(t, got, "new project must not see default's instances")
	profs, err := f.ListProfiles(dev)
	require.NoError(t, err)
	require.Len(t, profs, 1)
	assert.Equal(t, "default", profs[0].Name)
	assert.Empty(t, profs[0].Devices)

	require.NoError(t, f.CreateInstance(dev, backend.CreateOptions{Name: "c-dev", Image: "alpine/edge"}))
	devList, err := f.ListInstances(dev)
	require.NoError(t, err)
	require.Len(t, devList, 1)
	defList, err := f.ListInstances(ctx())
	require.NoError(t, err)
	require.Len(t, defList, 1)
	assert.Equal(t, "c-default", defList[0].Name, "default project must not see dev's instances")

	// Same instance name in both projects is fine.
	require.NoError(t, f.CreateInstance(dev, backend.CreateOptions{Name: "c-default", Image: "alpine/edge"}))

	// Images are isolated too (features.images defaults true).
	imgs, err := f.ListLocalImages(dev)
	require.NoError(t, err)
	assert.Empty(t, imgs, "default's image store is not shared into dev")

	// Networks are shared from default (features.networks unset): the dev
	// project sees and uses incusbr0.
	nets, err := f.ListNetworks(dev)
	require.NoError(t, err)
	require.NotEmpty(t, nets)

	// Volumes are per project.
	require.NoError(t, f.CreateVolume(dev, "default", backend.StorageVolume{Name: "v1"}))
	vols, err := f.ListVolumes(ctx(), "default")
	require.NoError(t, err)
	assert.Empty(t, vols, "dev's volumes are invisible to the default project")

	// A project owning its networks gets an isolated namespace.
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "netty", Description: "", Config: map[string]string{"features.networks": "true"}}))
	netty := backend.WithProject(ctx(), "netty")
	nets, err = f.ListNetworks(netty)
	require.NoError(t, err)
	assert.Empty(t, nets, "features.networks=true projects own an empty network namespace")

	// The non-empty guard sees project resources; cleanup frees it.
	require.ErrorIs(t, f.DeleteProject(ctx(), "dev"), backend.ErrConflict)
	require.NoError(t, f.DeleteInstance(dev, "c-dev"))
	require.NoError(t, f.DeleteInstance(dev, "c-default"))
	require.NoError(t, f.DeleteVolume(dev, "default", "v1"))
	require.NoError(t, f.DeleteProject(ctx(), "dev"))
}

// Checkpoint regressions: feature flips are frozen on non-empty projects,
// and isolated namespaces don't cross-guard same-named resources.
func TestProjectFeatureAndNamespaceGuards(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "dev", Description: ""}))
	dev := backend.WithProject(ctx(), "dev")

	// Feature flip on the (non-empty: has an instance) project is refused;
	// non-feature config edits still pass.
	require.NoError(t, f.CreateInstance(dev, backend.CreateOptions{Name: "c1", Image: "alpine/edge"}))
	p, err := f.GetProject(ctx(), "dev")
	require.NoError(t, err)
	flipped := maps.Clone(p.Config)
	flipped["features.images"] = "false"
	require.ErrorIs(t, f.UpdateProject(ctx(), "dev", "", flipped, p.Version), backend.ErrInvalid)
	same := maps.Clone(p.Config)
	same["user.note"] = "ok"
	require.NoError(t, f.UpdateProject(ctx(), "dev", "", same, p.Version))

	// dev shares default's profiles (features.profiles injected true — but
	// c1 uses dev's own default profile). A second project owning its
	// networks can reuse the shared network's name without blocking it.
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "netty", Description: "", Config: map[string]string{"features.networks": "true"}}))
	netty := backend.WithProject(ctx(), "netty")
	require.NoError(t, f.CreateNetwork(netty, backend.Network{Name: "incusbr0", Type: "bridge", Managed: true}))
	// Deleting netty's incusbr0 must not be blocked by default-project
	// instances attached to the shared incusbr0.
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "attached", Image: "alpine/edge"}))
	require.NoError(t, f.AddDevice(ctx(), "attached", "eth9", map[string]string{"type": "nic", "network": "incusbr0"}))
	require.NoError(t, f.DeleteNetwork(netty, "incusbr0"))

	// Publishing a scoped instance stores the image in dev's own space.
	require.NoError(t, f.StopInstance(dev, "c1"))
	require.NoError(t, f.PublishImage(dev, "c1", "dev-img"))
	imgs, err := f.ListLocalImages(dev)
	require.NoError(t, err)
	require.Len(t, imgs, 1)
	defImgs, err := f.ListLocalImages(ctx())
	require.NoError(t, err)
	for _, img := range defImgs {
		assert.NotContains(t, img.Aliases, "dev-img", "published image leaked into default")
	}
}

func TestGetProjectUsageReportsCountsAndLimits(t *testing.T) {
	f := New()

	// Default project: one created instance, the seeded managed network
	// (unmanaged eth0 doesn't count), no limits configured, rows sorted.
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "usage-inst", Image: "debian/12"}))
	usage, err := f.GetProjectUsage(ctx(), "default")
	require.NoError(t, err)
	byName := map[string]backend.ProjectUsage{}
	order := make([]string, 0, len(usage))
	for _, u := range usage {
		byName[u.Resource] = u
		order = append(order, u.Resource)
	}
	assert.True(t, sort.StringsAreSorted(order), "rows must be sorted by resource: %v", order)
	assert.Equal(t, int64(1), byName["instances"].Usage)
	assert.Equal(t, int64(-1), byName["instances"].Limit, "unset limit must be -1")
	assert.Equal(t, int64(1), byName["networks"].Usage)
	assert.Equal(t, int64(0), byName["memory"].Usage)

	// Instance limits aggregate into cpu/memory usage.
	require.NoError(t, f.UpdateLimits(ctx(), "usage-inst", backend.Limits{CPU: "2", Memory: "512MiB"}))
	usage, err = f.GetProjectUsage(ctx(), "default")
	require.NoError(t, err)
	for _, u := range usage {
		byName[u.Resource] = u
	}
	assert.Equal(t, int64(2), byName["cpu"].Usage)
	assert.Equal(t, int64(512<<20), byName["memory"].Usage)

	// Project limits.* config keys surface as parsed limits.
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "capped", Description: "", Config: map[string]string{
		"limits.instances": "5",
		"limits.memory":    "1GiB",
	}}))
	usage, err = f.GetProjectUsage(ctx(), "capped")
	require.NoError(t, err)
	for _, u := range usage {
		byName[u.Resource] = u
	}
	assert.Equal(t, int64(0), byName["instances"].Usage)
	assert.Equal(t, int64(5), byName["instances"].Limit)
	assert.Equal(t, int64(1<<30), byName["memory"].Limit)

	_, err = f.GetProjectUsage(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
