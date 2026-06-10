package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/adam/lxcon/internal/backend"
	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestZipConfigPairsDropsBlankKeys(t *testing.T) {
	got := zipConfigPairs([]string{"security.nesting", "", "boot.autostart"}, []string{"true", "ignored", "1"})
	assert.Equal(t, map[string]string{"security.nesting": "true", "boot.autostart": "1"}, got)

	got = zipConfigPairs([]string{"a", "b"}, []string{"x"})
	assert.Equal(t, map[string]string{"a": "x", "b": ""}, got)
}

func TestConfigPanelRenders(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.UpdateInstanceConfig(t.Context(), "demo", map[string]string{"security.nesting": "true"}))
	res := request(t, New(b), "GET", "/instances/demo/config", "", true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "security.nesting")
}

func TestDevicesPanelRenders(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := request(t, New(b), "GET", "/instances/demo/devices", "", true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `id="devices"`)
	assert.Contains(t, body, "root")     // inherited device from the default profile
	assert.Contains(t, body, "Add disk") // typed add form
}

func TestUpdateConfigAppliesAndReturnsPanel(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/config",
		url.Values{"key": {"security.nesting", ""}, "value": {"true", ""}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "security.nesting")
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "true", cfg.Config["security.nesting"])
}

func TestConfigPanelValueTextareaEscapesContent(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.UpdateInstanceConfig(t.Context(), "demo", map[string]string{
		"user.user-data": "#cloud-config\npackages:\n  - htop",
		"user.evil":      "</textarea><script>boom()</script>",
	}))
	res := request(t, New(b), "GET", "/instances/demo/config", "", true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `<textarea name="value"`)
	// Multiline value rendered as element text, newlines intact.
	assert.Contains(t, body, "#cloud-config\npackages:\n  - htop")
	// A value containing a closing tag must be escaped, not break out.
	assert.Contains(t, body, "&lt;/textarea&gt;")
	assert.NotContains(t, body, "<script>boom()")
}

func TestUpdateConfigMultilineValueRoundTrips(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	// Browsers submit textarea newlines as CRLF; stored values must be LF.
	res := formRequest(t, New(b), "/instances/demo/config",
		url.Values{"key": {"user.user-data"}, "value": {"#cloud-config\r\nruncmd:\r\n  - ls"}}, true)
	assertStatus(t, res, http.StatusOK)
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "#cloud-config\nruncmd:\n  - ls", cfg.Config["user.user-data"])
}

func TestConfigPanelUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/instances/ghost/config", "", true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestDeviceConfigFromFormDropsBlanks(t *testing.T) {
	got := deviceConfigFromForm("proxy", url.Values{
		"listen":  {"tcp:0.0.0.0:80"},
		"connect": {"tcp:127.0.0.1:80"},
		"bind":    {""},        // dropped
		"path":    {"ignored"}, // not a proxy field
	})
	assert.Equal(t, map[string]string{
		"type":    "proxy",
		"listen":  "tcp:0.0.0.0:80",
		"connect": "tcp:127.0.0.1:80",
	}, got)
}

func TestAddDeviceAppliesAndReturnsDevices(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/devices",
		url.Values{"type": {"proxy"}, "device": {"web"},
			"listen": {"tcp:0.0.0.0:80"}, "connect": {"tcp:127.0.0.1:80"}}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), "web")
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "proxy", cfg.LocalDevices["web"]["type"])
}

func TestRemoveDeviceAppliesAndReturnsDevices(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.AddDevice(t.Context(), "demo", "web", map[string]string{"type": "proxy"}))
	res := formRequest(t, New(b), "/instances/demo/devices/web/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	assert.NotContains(t, cfg.LocalDevices, "web")
}

func TestAddDeviceBlankNameIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/devices",
		url.Values{"type": {"proxy"}, "device": {""}, "listen": {"tcp::80"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestRemoveUnknownDeviceIs404(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/devices/ghost/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestUpdateDeviceMergesKnownFieldsPreservesUnknown(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.AddDevice(t.Context(), "demo", "web", map[string]string{
		"type": "proxy", "listen": "tcp:0.0.0.0:80", "connect": "tcp:127.0.0.1:80",
		"limits.ingress": "10Mbit", // not a typed form field; must survive
	}))
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)

	// New listen value, blank connect (= remove), bind untouched-blank (absent
	// before, stays absent).
	res := formRequest(t, New(b), "/instances/demo/devices/web",
		url.Values{"version": {cfg.Version}, "listen": {"tcp:0.0.0.0:8080"}, "connect": {""}, "bind": {""}}, true)
	assertStatus(t, res, http.StatusOK)

	got, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	dev := got.LocalDevices["web"]
	assert.Equal(t, "tcp:0.0.0.0:8080", dev["listen"])
	assert.Equal(t, "10Mbit", dev["limits.ingress"], "unknown keys must be preserved")
	assert.Equal(t, "proxy", dev["type"], "type must be preserved")
	assert.NotContains(t, dev, "connect", "blank known field removes the key")
}

func TestUpdateDeviceHandlerStaleVersionIs409(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.AddDevice(t.Context(), "demo", "web", map[string]string{"type": "proxy", "listen": "a"}))
	cfg, err := b.GetInstanceConfig(t.Context(), "demo")
	require.NoError(t, err)
	require.NoError(t, b.UpdateDevice(t.Context(), "demo", "web", map[string]string{"type": "proxy", "listen": "b"}, cfg.Version))

	res := formRequest(t, New(b), "/instances/demo/devices/web",
		url.Values{"version": {cfg.Version}, "listen": {"c"}}, true)
	assertStatus(t, res, http.StatusConflict)
}

func TestUpdateDeviceUnknownDeviceIs404(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	res := formRequest(t, New(b), "/instances/demo/devices/ghost", url.Values{"version": {"0"}, "listen": {"x"}}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestDevicesPanelRendersEditForms(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.AddDevice(t.Context(), "demo", "web", map[string]string{"type": "proxy", "listen": "tcp:0.0.0.0:80"}))
	res := request(t, New(b), "GET", "/instances/demo/devices", "", true)
	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	assert.Contains(t, body, `hx-post="/instances/demo/devices/web"`)
	assert.Contains(t, body, `name="version"`)
	assert.Contains(t, body, `value="tcp:0.0.0.0:80"`) // pre-filled field
}

func TestUpdateDeviceMissingVersionIs400(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateInstance(t.Context(), backend.CreateOptions{Name: "demo"}))
	require.NoError(t, b.AddDevice(t.Context(), "demo", "web", map[string]string{"type": "proxy"}))
	res := formRequest(t, New(b), "/instances/demo/devices/web", url.Values{"listen": {"x"}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}
