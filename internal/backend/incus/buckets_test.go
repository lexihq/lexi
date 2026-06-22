package incus

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListBucketsMapsAndSorts(t *testing.T) {
	s := &instanceServerStub{buckets: []api.StorageBucket{
		{Name: "media", S3URL: "https://127.0.0.1:8555/media", StorageBucketPut: api.StorageBucketPut{
			Description: "app assets",
			Config:      api.ConfigMap{"size": "100MiB"},
		}},
		{Name: "backups", S3URL: "https://127.0.0.1:8555/backups"},
	}}
	b := &incusBackend{srv: s}

	got, err := b.ListBuckets(t.Context(), "default")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "backups", got[0].Name, "buckets must sort by name")
	assert.Equal(t, backend.StorageBucket{
		Name:        "media",
		Description: "app assets",
		S3URL:       "https://127.0.0.1:8555/media",
		Size:        "100MiB",
	}, got[1])
	assert.Equal(t, "default", s.bucketPool)
}

func TestCreateBucketSendsSizeConfig(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}

	require.NoError(t, b.CreateBucket(t.Context(), "default", backend.StorageBucket{Name: "media", Description: "d", Size: "100MiB"}))
	require.NotNil(t, s.createdBucket)
	assert.Equal(t, "media", s.createdBucket.Name)
	assert.Equal(t, "d", s.createdBucket.Description)
	assert.Equal(t, api.ConfigMap{"size": "100MiB"}, s.createdBucket.Config)

	// No size → no config key (the daemon treats empty values as unset).
	require.NoError(t, b.CreateBucket(t.Context(), "default", backend.StorageBucket{Name: "media2", Description: ""}))
	assert.Empty(t, s.createdBucket.Config)
}

func TestDeleteBucketPassesNames(t *testing.T) {
	s := &instanceServerStub{}
	b := &incusBackend{srv: s}
	require.NoError(t, b.DeleteBucket(t.Context(), "default", "media"))
	assert.Equal(t, "default", s.bucketPool)
	assert.Equal(t, "media", s.deletedBucket)
}

func TestBucketKeysRoundTrip(t *testing.T) {
	s := &instanceServerStub{bucketKeys: []api.StorageBucketKey{
		{Name: "admin", StorageBucketKeyPut: api.StorageBucketKeyPut{
			Role: "admin", AccessKey: "AK1", SecretKey: "SK1",
		}},
	}}
	b := &incusBackend{srv: s}

	keys, err := b.ListBucketKeys(t.Context(), "default", "media")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, backend.BucketKey{Name: "admin", Role: "admin", AccessKey: "AK1", SecretKey: "SK1"}, keys[0])

	key, err := b.CreateBucketKey(t.Context(), "default", "media", "ci", "ci reader", "read-only")
	require.NoError(t, err)
	require.NotNil(t, s.createdBucketKey)
	assert.Equal(t, "ci", s.createdBucketKey.Name)
	assert.Equal(t, "read-only", s.createdBucketKey.Role)
	// The daemon's generated credentials come back to the caller.
	assert.Equal(t, "GENERATED-ACCESS", key.AccessKey)
	assert.Equal(t, "GENERATED-SECRET", key.SecretKey)

	require.NoError(t, b.DeleteBucketKey(t.Context(), "default", "media", "ci"))
	assert.Equal(t, "ci", s.deletedBucketKey)
}
