package fake

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestImportInstanceRoundTrip(t *testing.T) {
	b := New()
	mustCreate(t, b, "demo") // image debian/12

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))

	// A round-trip recreates the instance under a new name, preserving the image.
	require.NoError(t, b.ImportInstance(ctx(), "restored", &buf))
	inst, err := b.GetInstance(ctx(), "restored")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)

	// Importing onto an existing name → ErrConflict.
	buf.Reset()
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))
	require.ErrorIs(t, b.ImportInstance(ctx(), "demo", &buf), backend.ErrConflict)

	// A blob that isn't a lexi backup → ErrInvalid.
	require.ErrorIs(t, b.ImportInstance(ctx(), "x", strings.NewReader("garbage")), backend.ErrInvalid)
}

func TestExportInstance(t *testing.T) {
	b := New()
	assert.True(t, b.Capabilities(ctx()).Backup, "fake should advertise backup")
	mustCreate(t, b, "demo")

	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(ctx(), "demo", &buf))
	assert.NotEmpty(t, buf.Bytes(), "export should write a backup blob")

	// Missing instance → ErrNotFound.
	require.ErrorIs(t, b.ExportInstance(ctx(), "ghost", &buf), backend.ErrNotFound)
}
