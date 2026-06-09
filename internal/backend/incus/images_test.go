package incus

import (
	"testing"

	"github.com/lxc/incus/v6/shared/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToImagesKeepsDistinctImageTypes(t *testing.T) {
	images := []api.Image{
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "container-fingerprint",
			Type:         "container",
		},
		{
			Aliases:      []api.ImageAlias{{Name: "debian/12"}},
			Architecture: "x86_64",
			Fingerprint:  "vm-fingerprint",
			Type:         "virtual-machine",
		},
	}

	got := toImages(images)

	require.Len(t, got, 2)
	assert.Equal(t, "container-fingerprint", got[0].Fingerprint)
	assert.Equal(t, "container", got[0].Type)
	assert.Equal(t, "vm-fingerprint", got[1].Fingerprint)
	assert.Equal(t, "virtual-machine", got[1].Type)
}
