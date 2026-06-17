package fake

import (
	"context"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func secondaryCtx() context.Context {
	return backend.WithRemote(context.Background(), "secondary")
}

func TestListRemotesMarksCurrentFromContext(t *testing.T) {
	f := New()

	remotes, err := f.ListRemotes(ctx())
	require.NoError(t, err)
	require.Len(t, remotes, 2)
	assert.Equal(t, "local", remotes[0].Name)
	assert.True(t, remotes[0].Current, "default context selects local")
	assert.Equal(t, "secondary", remotes[1].Name)
	assert.False(t, remotes[1].Current)

	remotes, err = f.ListRemotes(secondaryCtx())
	require.NoError(t, err)
	assert.False(t, remotes[0].Current)
	assert.True(t, remotes[1].Current)
}

func TestRemotesIsolateState(t *testing.T) {
	f := New()

	// The secondary remote starts without the local remote's seeds beyond its
	// own defaults, and resources created there stay there.
	require.NoError(t, f.CreateInstance(secondaryCtx(), backend.CreateOptions{Name: "remote-inst", Image: "debian/12"}))

	sec, err := f.ListInstances(secondaryCtx())
	require.NoError(t, err)
	require.Len(t, sec, 1)
	assert.Equal(t, "remote-inst", sec[0].Name)

	local, err := f.ListInstances(ctx())
	require.NoError(t, err)
	for _, in := range local {
		assert.NotEqual(t, "remote-inst", in.Name, "secondary instance leaked into local")
	}

	// Server-level state is per remote too.
	pemData, _ := testCertPEM(t)
	require.NoError(t, f.AddCertificate(secondaryCtx(), "sec-cert", "client", pemData))
	localCerts, err := f.ListCertificates(ctx())
	require.NoError(t, err)
	for _, c := range localCerts {
		assert.NotEqual(t, "sec-cert", c.Name, "secondary certificate leaked into local")
	}
}

func TestMigrateInstanceMovesAcrossRemotes(t *testing.T) {
	f := New()
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "mig", Image: "debian/12"}))

	// Running instances must be stopped first.
	require.NoError(t, f.StartInstance(ctx(), "mig"))
	err := f.MigrateInstance(ctx(), "mig", "secondary", "")
	require.ErrorIs(t, err, backend.ErrInvalid)
	require.NoError(t, f.StopInstance(ctx(), "mig"))

	// Unknown target remote.
	err = f.MigrateInstance(ctx(), "mig", "ghost", "")
	require.ErrorIs(t, err, backend.ErrNotFound)

	// Happy path with rename: gone from local, present on secondary.
	require.NoError(t, f.MigrateInstance(ctx(), "mig", "secondary", "mig2"))
	_, err = f.GetInstance(ctx(), "mig")
	require.ErrorIs(t, err, backend.ErrNotFound)
	inst, err := f.GetInstance(secondaryCtx(), "mig2")
	require.NoError(t, err)
	assert.Equal(t, "Stopped", inst.Status)

	// Name conflicts on the target are rejected and the source is kept.
	require.NoError(t, f.CreateInstance(ctx(), backend.CreateOptions{Name: "mig2", Image: "debian/12"}))
	err = f.MigrateInstance(ctx(), "mig2", "secondary", "")
	require.ErrorIs(t, err, backend.ErrConflict)
	if _, err := f.GetInstance(ctx(), "mig2"); err != nil {
		t.Fatalf("source must survive a failed migration: %v", err)
	}
}
