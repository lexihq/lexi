package fake

import (
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpdateImageLifecycleFields(t *testing.T) {
	f := New()
	const fp = "fake-debian-12-aarch64" // seeded local image

	expiry := time.Date(2027, time.March, 1, 0, 0, 0, 0, time.UTC)
	require.NoError(t, f.UpdateImage(ctx(), fp, backend.ImageEdit{
		Description: "edited", Public: true, AutoUpdate: true, ExpiresAt: expiry,
	}))

	imgs, err := f.ListLocalImages(ctx())
	require.NoError(t, err)
	var got *backend.LocalImage
	for i := range imgs {
		if imgs[i].Fingerprint == fp {
			got = &imgs[i]
		}
	}
	require.NotNil(t, got)
	assert.Equal(t, "edited", got.Description)
	assert.True(t, got.Public)
	assert.True(t, got.AutoUpdate)
	assert.Equal(t, expiry, got.ExpiresAt)
}

func TestRefreshImageNeedsUpdateSource(t *testing.T) {
	f := New()

	// The seeded image has no update source (like a published one).
	err := f.RefreshImage(ctx(), "fake-debian-12-aarch64")
	require.ErrorIs(t, err, backend.ErrInvalid)

	// A copy from the remote catalog has one and refreshes fine.
	require.NoError(t, f.CopyImage(ctx(), "ubuntu/24.04"))
	imgs, err := f.ListLocalImages(ctx())
	require.NoError(t, err)
	var copied string
	for _, img := range imgs {
		for _, a := range img.Aliases {
			if a == "ubuntu/24.04" {
				copied = img.Fingerprint
			}
		}
	}
	require.NotEmpty(t, copied)
	require.NoError(t, f.RefreshImage(ctx(), copied))

	err = f.RefreshImage(ctx(), "ghost-fp")
	require.ErrorIs(t, err, backend.ErrNotFound)
}
