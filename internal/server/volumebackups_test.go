package server

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestVolumeBackupHandlers(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateStoragePool(context.Background(), backend.StoragePool{Name: "pool", Driver: "dir"}))
	require.NoError(t, b.CreateVolume(context.Background(), "pool", backend.StorageVolume{Name: "vol"}))
	srv := New(b)

	// Create with a name and volume-only flag.
	res := formRequest(t, srv, "/storage/pool/volumes/vol/backups", url.Values{"name": {"weekly"}, "volume_only": {"on"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	bks, err := b.ListVolumeBackups(context.Background(), "pool", "vol")
	require.NoError(t, err)
	require.Len(t, bks, 1)
	assert.True(t, bks[0].VolumeOnly)

	// The lazy panel lists it.
	res = request(t, srv, "GET", "/storage/pool/volumes/vol/backups", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "weekly")

	// Download streams the tarball with a filename.
	res = request(t, srv, "GET", "/storage/pool/volumes/vol/backups/weekly/export", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Header().Get("Content-Disposition"), "vol-weekly")
	assert.NotZero(t, res.Body.Len())

	// Restore-as requires a new name, then creates the volume in the target pool.
	res = formRequest(t, srv, "/storage/pool/volumes/vol/backups/weekly/restore", url.Values{"target_pool": {"pool"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
	res = formRequest(t, srv, "/storage/pool/volumes/vol/backups/weekly/restore", url.Values{"target_pool": {"pool"}, "name": {"vol2"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	if !strings.HasPrefix(res.Header().Get("Location"), "/storage/pool/volumes/vol2") {
		t.Fatalf("restore should land on the new volume, got %q", res.Header().Get("Location"))
	}
	_, err = b.GetVolume(context.Background(), "pool", "vol2")
	require.NoError(t, err)

	// Restore-as with no target_pool falls back to the source pool.
	res = formRequest(t, srv, "/storage/pool/volumes/vol/backups/weekly/restore", url.Values{"name": {"vol3"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	if !strings.HasPrefix(res.Header().Get("Location"), "/storage/pool/volumes/vol3") {
		t.Fatalf("restore without target_pool should land in the source pool, got %q", res.Header().Get("Location"))
	}
	_, err = b.GetVolume(context.Background(), "pool", "vol3")
	require.NoError(t, err)

	// Delete.
	res = formRequest(t, srv, "/storage/pool/volumes/vol/backups/weekly/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	bks, err = b.ListVolumeBackups(context.Background(), "pool", "vol")
	require.NoError(t, err)
	assert.Empty(t, bks)
}
