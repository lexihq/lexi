package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestServerPageRendersAllSections(t *testing.T) {
	res := request(t, New(fake.New()), "GET", "/server", "", false)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, "6.0-fake")               // overview
	assert.Contains(t, body, "core.https_address")     // config row
	assert.Contains(t, body, "admin-laptop")           // certificate
	assert.Contains(t, body, "KVM support is missing") // warning message
}

func TestServerConfigApplyReplacesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/server/config",
		url.Values{"key": {"user.greeting", ""}, "value": {"hi", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)

	cfg, err := b.GetServerConfig(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"user.greeting": "hi"}, cfg)
}

func TestDeleteWarningRemovesAndReturnsTable(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/server/warnings/fake-warning-1/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.NotContains(t, res.Body.String(), "fake-warning-1")

	warnings, err := b.ListWarnings(t.Context())
	require.NoError(t, err)
	require.Len(t, warnings, 1)
}

func TestDeleteWarningGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/warnings/ghost/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}
