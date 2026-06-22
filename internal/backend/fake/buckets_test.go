package fake

import (
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBucketCRUDRoundTrip(t *testing.T) {
	f := New()

	buckets, err := f.ListBuckets(ctx(), "default")
	require.NoError(t, err)
	require.Empty(t, buckets)
	_, err = f.ListBuckets(ctx(), "ghost")
	require.ErrorIs(t, err, backend.ErrNotFound)

	require.NoError(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "media", Description: "app assets", Size: "100MiB"}))
	require.ErrorIs(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "media", Description: ""}), backend.ErrConflict)
	require.ErrorIs(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "bad name", Description: ""}), backend.ErrInvalid)
	require.ErrorIs(t, f.CreateBucket(ctx(), "ghost", backend.StorageBucket{Name: "media", Description: ""}), backend.ErrNotFound)

	buckets, err = f.ListBuckets(ctx(), "default")
	require.NoError(t, err)
	require.Len(t, buckets, 1)
	assert.Equal(t, "media", buckets[0].Name)
	assert.Equal(t, "app assets", buckets[0].Description)
	assert.Equal(t, "100MiB", buckets[0].Size)
	assert.NotEmpty(t, buckets[0].S3URL)

	require.NoError(t, f.DeleteBucket(ctx(), "default", "media"))
	require.ErrorIs(t, f.DeleteBucket(ctx(), "default", "media"), backend.ErrNotFound)
}

func TestBucketKeysLifecycle(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "media", Description: ""}))

	// Daemon parity: bucket creation seeds an admin key.
	keys, err := f.ListBucketKeys(ctx(), "default", "media")
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "admin", keys[0].Name)
	assert.Equal(t, "admin", keys[0].Role)
	assert.NotEmpty(t, keys[0].AccessKey)
	assert.NotEmpty(t, keys[0].SecretKey)

	// New keys default to read-only and return generated credentials.
	key, err := f.CreateBucketKey(ctx(), "default", "media", "ci", "ci reader", "")
	require.NoError(t, err)
	assert.Equal(t, "read-only", key.Role)
	assert.NotEmpty(t, key.AccessKey)
	assert.NotEmpty(t, key.SecretKey)
	assert.NotEqual(t, keys[0].AccessKey, key.AccessKey, "credentials must be unique")

	_, err = f.CreateBucketKey(ctx(), "default", "media", "ci", "", "")
	require.ErrorIs(t, err, backend.ErrConflict)
	_, err = f.CreateBucketKey(ctx(), "default", "media", "x", "", "owner")
	require.ErrorIs(t, err, backend.ErrInvalid, "role must be admin or read-only")
	_, err = f.CreateBucketKey(ctx(), "default", "ghost", "x", "", "")
	require.ErrorIs(t, err, backend.ErrNotFound)

	keys, err = f.ListBucketKeys(ctx(), "default", "media")
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, "admin", keys[0].Name, "keys must sort by name")
	assert.Equal(t, "ci", keys[1].Name)

	require.NoError(t, f.DeleteBucketKey(ctx(), "default", "media", "ci"))
	require.ErrorIs(t, f.DeleteBucketKey(ctx(), "default", "media", "ci"), backend.ErrNotFound)

	// Keys vanish with their bucket.
	require.NoError(t, f.DeleteBucket(ctx(), "default", "media"))
	require.NoError(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "media", Description: ""}))
	keys, err = f.ListBucketKeys(ctx(), "default", "media")
	require.NoError(t, err)
	require.Len(t, keys, 1, "only the fresh admin key may exist")
}

func TestBucketsAreProjectScopedByFeature(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateBucket(ctx(), "default", backend.StorageBucket{Name: "shared", Description: ""}))

	// A project without its own bucket feature shares default's buckets.
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "plain", Description: ""}))
	buckets, err := f.ListBuckets(backend.WithProject(ctx(), "plain"), "default")
	require.NoError(t, err)
	require.Len(t, buckets, 1)

	// A project owning features.storage.buckets gets its own namespace.
	require.NoError(t, f.CreateProject(ctx(), backend.Project{Name: "bucketed", Description: "", Config: map[string]string{"features.storage.buckets": "true"}}))
	buckets, err = f.ListBuckets(backend.WithProject(ctx(), "bucketed"), "default")
	require.NoError(t, err)
	require.Empty(t, buckets)
	require.NoError(t, f.CreateBucket(backend.WithProject(ctx(), "bucketed"), "default", backend.StorageBucket{Name: "shared", Description: "same name, own namespace"}))
}
