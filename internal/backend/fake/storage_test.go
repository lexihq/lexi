package fake

import (
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoragePoolsAndVolumes(t *testing.T) {
	f := New()

	pools, err := f.ListStoragePools(ctx())
	require.NoError(t, err)
	assert.NotEmpty(t, pools) // seeded default + zfs0

	p, err := f.GetStoragePool(ctx(), "default")
	require.NoError(t, err)
	assert.Equal(t, "dir", p.Driver)

	require.NoError(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1", ContentType: "filesystem", Config: map[string]string{"size": "1GiB"}}))
	vols, err := f.ListVolumes(ctx(), "default")
	require.NoError(t, err)
	assert.Len(t, vols, 1)

	v, err := f.GetVolume(ctx(), "default", "vol1")
	require.NoError(t, err)
	assert.Equal(t, "custom", v.Type)
	assert.Equal(t, "1GiB", v.Config["size"])

	require.ErrorIs(t, f.CreateVolume(ctx(), "default", backend.StorageVolume{Name: "vol1"}), backend.ErrConflict)
	require.ErrorIs(t, f.CreateVolume(ctx(), "ghost", backend.StorageVolume{Name: "x"}), backend.ErrNotFound)

	require.NoError(t, f.DeleteVolume(ctx(), "default", "vol1"))
	require.ErrorIs(t, f.DeleteVolume(ctx(), "default", "vol1"), backend.ErrNotFound)
	_, err = f.GetStoragePool(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
