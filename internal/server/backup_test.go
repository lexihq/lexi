package server

import (
	"bytes"
	"net/http"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExportDownloadsTarball(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))

	res := request(t, New(b), "GET", "/instances/demo/export", "", false)

	assert.Equal(t, http.StatusOK, res.Code)
	assert.Equal(t, "application/octet-stream", res.Header().Get("Content-Type"))
	assert.Contains(t, res.Header().Get("Content-Disposition"), `filename="demo.tar.gz"`)
	assert.NotEmpty(t, res.Body.Bytes())
}

func TestExportUnknownInstanceIs404(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/ghost/export", "", false)
	assert.Equal(t, http.StatusNotFound, res.Code)
}

func TestImportFormRenders(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/instances/import", "", false)
	assert.Equal(t, http.StatusOK, res.Code)
	assert.Contains(t, res.Body.String(), "Import instance")
}

func TestImportCreatesInstanceFromUpload(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo", Image: "debian/12"}))
	var buf bytes.Buffer
	require.NoError(t, b.ExportInstance(t.Context(), "demo", &buf))

	res := importRequest(t, New(b), "restored", buf.Bytes(), false)

	assert.Equal(t, http.StatusSeeOther, res.Code)
	assert.Equal(t, "/", res.Header().Get("Location"))
	inst, err := b.GetInstance(t.Context(), "restored")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)
}

func TestImportValidatesNameAndFile(t *testing.T) {
	t.Run("missing name", func(t *testing.T) {
		res := importRequest(t, New(fake.New()), "", []byte("x"), true)
		assert.Equal(t, http.StatusBadRequest, res.Code)
		assert.Contains(t, res.Body.String(), "name is required")
	})

	t.Run("missing file", func(t *testing.T) {
		res := importRequest(t, New(fake.New()), "restored", nil, true)
		assert.Equal(t, http.StatusBadRequest, res.Code)
		assert.Contains(t, res.Body.String(), "backup file is required")
	})
}

func TestImportRejectsInvalidBackup(t *testing.T) {
	res := importRequest(t, New(fake.New()), "restored", []byte("garbage"), true)
	assert.Equal(t, http.StatusBadRequest, res.Code)
}

func TestImportRejectsOversizedUpload(t *testing.T) {
	orig := maxImportBytes
	maxImportBytes = 16
	t.Cleanup(func() { maxImportBytes = orig })

	res := importRequest(t, New(fake.New()), "restored", make([]byte, 1<<10), true)

	assert.Equal(t, http.StatusRequestEntityTooLarge, res.Code)
	assert.Contains(t, res.Body.String(), "too large")
}
