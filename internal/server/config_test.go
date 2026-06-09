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
	body := res.Body.String()
	assert.Contains(t, body, "security.nesting")
	assert.Contains(t, body, "root") // device from default profile
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

func TestConfigPanelUnknownInstanceIs404(t *testing.T) {
	b := fake.New()
	res := request(t, New(b), "GET", "/instances/ghost/config", "", true)
	assertStatus(t, res, http.StatusNotFound)
}
