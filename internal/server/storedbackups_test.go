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

func TestStoredBackupHandlers(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(context.Background(), backend.CreateOptions{Name: "bk", Image: "debian/12"}))
	srv := New(b)

	// Create with a name and instance-only flag.
	res := formRequest(t, srv, "/instances/bk/backups", url.Values{"name": {"weekly"}, "instance_only": {"on"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	bks, err := b.ListInstanceBackups(context.Background(), "bk")
	require.NoError(t, err)
	require.Len(t, bks, 1)
	assert.True(t, bks[0].InstanceOnly)

	// The lazy panel lists it.
	res = request(t, srv, "GET", "/instances/bk/backups", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "weekly")

	// Download streams the tarball with a filename.
	res = request(t, srv, "GET", "/instances/bk/backups/weekly/download", "", false)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Header().Get("Content-Disposition"), "bk-weekly")
	assert.NotZero(t, res.Body.Len())

	// Restore-as requires a new name, then creates the instance.
	res = formRequest(t, srv, "/instances/bk/backups/weekly/restore", url.Values{}, false)
	assertStatus(t, res, http.StatusBadRequest)
	res = formRequest(t, srv, "/instances/bk/backups/weekly/restore", url.Values{"new_name": {"bk2"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	if !strings.HasPrefix(res.Header().Get("Location"), "/instances/bk2") {
		t.Fatalf("restore should land on the new instance, got %q", res.Header().Get("Location"))
	}
	_, err = b.GetInstance(context.Background(), "bk2")
	require.NoError(t, err)

	// Delete.
	res = formRequest(t, srv, "/instances/bk/backups/weekly/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	bks, err = b.ListInstanceBackups(context.Background(), "bk")
	require.NoError(t, err)
	assert.Empty(t, bks)
}
