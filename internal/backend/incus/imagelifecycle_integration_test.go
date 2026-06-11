//go:build integration

package incus

import (
	"context"
	"testing"
	"time"

	"github.com/adam/lxcon/internal/backend"
	"github.com/stretchr/testify/require"
)

// TestImageLifecycleFieldsIntegration edits auto-update/expiry on a remote-
// sourced image and refreshes it against the real daemon.
func TestImageLifecycleFieldsIntegration(t *testing.T) {
	b := newBackend(t)
	ctx := context.Background()

	// Creating an instance pulls testImage into the local store with an
	// update source.
	name := uniqueName("imgl")
	t.Cleanup(func() { cleanupInstance(t, b, name) })
	require.NoError(t, b.CreateInstance(ctx, backend.CreateOptions{Name: name, Image: testImage}))

	imgs, err := b.ListLocalImages(ctx)
	require.NoError(t, err)
	var img *backend.LocalImage
	for i := range imgs {
		if imgs[i].HasUpdateSource {
			img = &imgs[i]
			break
		}
	}
	require.NotNil(t, img, "expected a remote-sourced image in the local store")

	// Edit lifecycle fields, then restore the original state.
	orig := backend.ImageEdit{Description: img.Description, Public: img.Public, AutoUpdate: img.AutoUpdate, ExpiresAt: img.ExpiresAt}
	t.Cleanup(func() {
		if err := b.UpdateImage(ctx, img.Fingerprint, orig); err != nil {
			t.Logf("restore image fields: %v", err)
		}
	})
	expiry := time.Now().Add(48 * time.Hour).UTC().Truncate(time.Second)
	require.NoError(t, b.UpdateImage(ctx, img.Fingerprint, backend.ImageEdit{
		Description: img.Description, Public: img.Public, AutoUpdate: true, ExpiresAt: expiry,
	}))

	imgs, err = b.ListLocalImages(ctx)
	require.NoError(t, err)
	for _, got := range imgs {
		if got.Fingerprint == img.Fingerprint {
			require.True(t, got.AutoUpdate, "auto-update not applied")
			require.WithinDuration(t, expiry, got.ExpiresAt, time.Minute)
		}
	}

	if b.Capabilities(ctx).ImageRefresh {
		require.NoError(t, b.RefreshImage(ctx, img.Fingerprint))
	}
}
