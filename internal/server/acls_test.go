package server

import (
	"net/http"
	"net/url"
	"testing"

	"github.com/lexihq/lexi/internal/backend"
	"github.com/lexihq/lexi/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNetworkACLCreateRedirectsToDetail(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/network-acls", url.Values{"name": {"web"}, "description": {"web traffic"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/network-acls/web", res.Header().Get("Location"))

	acl, err := b.GetNetworkACL(t.Context(), "web")
	require.NoError(t, err)
	assert.Equal(t, "web traffic", acl.Description)
}

func TestNetworkACLCreateBlankNameIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/network-acls", url.Values{"name": {" "}}, true)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestNetworkACLPagesRender(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkACL(t.Context(), "web", "web traffic"))

	list := request(t, New(b), "GET", "/network-acls", "", false)
	assertStatus(t, list, http.StatusOK)
	assert.Contains(t, list.Body.String(), "web traffic")

	detail := request(t, New(b), "GET", "/network-acls/web", "", false)
	assertStatus(t, detail, http.StatusOK)
	assert.Contains(t, detail.Body.String(), "Ingress rules")
}

func TestNetworkACLAddAndDeleteRule(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkACL(t.Context(), "web", ""))
	acl, err := b.GetNetworkACL(t.Context(), "web")
	require.NoError(t, err)

	res := formRequest(t, New(b), "/network-acls/web/rules", url.Values{
		"direction": {"ingress"}, "action": {"allow"}, "protocol": {"tcp"},
		"destination_port": {"443"}, "state": {"enabled"}, "version": {acl.Version},
	}, false)
	assertStatus(t, res, http.StatusSeeOther)

	acl, err = b.GetNetworkACL(t.Context(), "web")
	require.NoError(t, err)
	require.Len(t, acl.Ingress, 1)
	assert.Equal(t, "443", acl.Ingress[0].DestinationPort)

	// Stale version conflicts; bad direction and bad index are 400.
	stale := formRequest(t, New(b), "/network-acls/web/rules", url.Values{
		"direction": {"egress"}, "action": {"allow"}, "state": {"enabled"}, "version": {"0"},
	}, true)
	assertStatus(t, stale, http.StatusConflict)
	bad := formRequest(t, New(b), "/network-acls/web/rules", url.Values{"direction": {"sideways"}}, true)
	assertStatus(t, bad, http.StatusBadRequest)
	noToken := formRequest(t, New(b), "/network-acls/web/rules", url.Values{"direction": {"ingress"}, "action": {"allow"}}, true)
	assertStatus(t, noToken, http.StatusBadRequest)

	res = formRequest(t, New(b), "/network-acls/web/rules/delete", url.Values{
		"direction": {"ingress"}, "index": {"0"}, "version": {acl.Version},
	}, false)
	assertStatus(t, res, http.StatusSeeOther)
	acl, err = b.GetNetworkACL(t.Context(), "web")
	require.NoError(t, err)
	assert.Empty(t, acl.Ingress)

	oob := formRequest(t, New(b), "/network-acls/web/rules/delete", url.Values{
		"direction": {"ingress"}, "index": {"5"}, "version": {acl.Version},
	}, true)
	assertStatus(t, oob, http.StatusBadRequest)
}

func TestNetworkACLRenameAndDelete(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkACL(t.Context(), "web", ""))

	res := formRequest(t, New(b), "/network-acls/web/rename", url.Values{"new_name": {"frontend"}}, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/network-acls/frontend", res.Header().Get("Location"))

	res = formRequest(t, New(b), "/network-acls/frontend/delete", url.Values{}, false)
	assertStatus(t, res, http.StatusSeeOther)
	_, err := b.GetNetworkACL(t.Context(), "frontend")
	require.ErrorIs(t, err, backend.ErrNotFound)
}

func TestNetworkACLDeleteReferencedIs409(t *testing.T) {
	b := fake.New()
	require.NoError(t, b.CreateNetworkACL(t.Context(), "web", ""))
	n, err := b.GetNetwork(t.Context(), "incusbr0")
	require.NoError(t, err)
	cfg := n.Config
	cfg["security.acls"] = "web"
	require.NoError(t, b.UpdateNetwork(t.Context(), "incusbr0", n.Description, cfg, ""))

	res := formRequest(t, New(b), "/network-acls/web/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusConflict)
}
