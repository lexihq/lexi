package fake

import (
	"context"
	"testing"

	"github.com/adam/lxcon/internal/backend"
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
