package incus

import (
	"bytes"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetProjectCarriesEtagAsVersion(t *testing.T) {
	p := &api.Project{Name: "dev"}
	p.Description = "dev project"
	p.Config = map[string]string{"features.profiles": "true"}
	b := &incusBackend{srv: &instanceServerStub{project: p}}

	got, err := b.GetProject(t.Context(), "dev")
	require.NoError(t, err)
	assert.Equal(t, "project-etag", got.Version)
	assert.Equal(t, "dev project", got.Description)
	assert.Equal(t, "true", got.Config["features.profiles"])
}

func TestCreateProjectSendsPost(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.CreateProject(t.Context(), "dev", "d", map[string]string{"features.images": "false"}))
	require.NotNil(t, s.createdProject)
	assert.Equal(t, "dev", s.createdProject.Name)
	assert.Equal(t, "d", s.createdProject.Description)
	assert.Equal(t, "false", s.createdProject.Config["features.images"])
}

func TestUpdateProjectSendsEtag(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.UpdateProject(t.Context(), "dev", "edited", map[string]string{"k": "v"}, "etag-1"))
	require.NotNil(t, s.updatedProject)
	assert.Equal(t, "edited", s.updatedProject.Description)
	assert.Equal(t, "etag-1", s.projectEtag)
}

func TestRenameProjectWaitsOperation(t *testing.T) {
	op := &operationStub{}
	s := &instanceServerStub{renameProjOp: op}
	b := &incusBackend{srv: s}
	require.NoError(t, b.RenameProject(t.Context(), "dev", "dev2"))
	assert.Equal(t, [2]string{"dev", "dev2"}, s.renamedProject)
	assert.True(t, op.waitContextUsed, "rename is async; the operation must be awaited")
}

func TestDefaultProjectGuards(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.ErrorIs(t, b.RenameProject(t.Context(), "default", "x"), backend.ErrInvalid)
	require.ErrorIs(t, b.DeleteProject(t.Context(), "default"), backend.ErrInvalid)
	assert.Empty(t, s.deletedProject, "the guard must fire before the daemon call")
}

func TestDeleteProjectCallsThrough(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteProject(t.Context(), "dev"))
	assert.Equal(t, "dev", s.deletedProject)
}

// The scoping contract: a project tagged on the ctx routes the call through
// the project-scoped client; unset or "default" uses the bare client. The
// export flows matter most — their downloads and deferred cleanups bypass the
// generic request builder (plan-review P3).
func TestContextProjectScopesClient(t *testing.T) {
	devCtx := backend.WithProject(t.Context(), "dev")

	t.Run("instance list", func(t *testing.T) {
		s := &instanceServerStub{}
		b := &incusBackend{srv: s}
		_, err := b.ListInstances(devCtx)
		require.NoError(t, err)
		assert.Equal(t, "dev", s.usedProject)
	})

	t.Run("instance export backup download and cleanup", func(t *testing.T) {
		s := &instanceServerStub{
			backupOp:       &operationStub{},
			backupDeleteOp: &operationStub{},
			backupBytes:    []byte("x"),
		}
		b := &incusBackend{srv: s}
		var buf bytes.Buffer
		require.NoError(t, b.ExportInstance(devCtx, "demo", &buf))
		assert.Equal(t, "dev", s.usedProject)
		assert.NotEmpty(t, s.deletedBackup, "scoped cleanup must still run")
	})

	t.Run("image export download", func(t *testing.T) {
		s := &instanceServerStub{
			image:         &api.Image{Type: "container"},
			imageFileMeta: []byte("meta"),
		}
		b := &incusBackend{srv: s}
		_, rc, err := b.ExportImage(devCtx, "fp")
		require.NoError(t, err)
		require.NoError(t, rc.Close())
		assert.Equal(t, "dev", s.usedProject)
	})

	t.Run("default project stays on the bare client", func(t *testing.T) {
		s := &instanceServerStub{}
		b := &incusBackend{srv: s}
		_, err := b.ListInstances(backend.WithProject(t.Context(), "default"))
		require.NoError(t, err)
		assert.Empty(t, s.usedProject)
		_, err = b.ListInstances(t.Context())
		require.NoError(t, err)
		assert.Empty(t, s.usedProject)
	})
}
