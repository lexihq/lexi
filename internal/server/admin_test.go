package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

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
	assert.Contains(t, body, `<textarea name="value"`) // multiline-capable editor
}

func TestServerConfigApplyReplacesAndRedirects(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/server/config",
		url.Values{"key": {"user.greeting", ""}, "value": {"hi", ""}}, false)
	assertStatus(t, res, http.StatusSeeOther)

	cfg, _, err := b.GetServerConfig(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"user.greeting": "hi"}, cfg)
}

func TestServerConfigStaleVersionIs409(t *testing.T) {
	b := fake.New()
	// Bump the config version behind the form's back.
	require.NoError(t, b.UpdateServerConfig(t.Context(), map[string]string{"user.other": "1"}, ""))

	res := formRequest(t, New(b), "/server/config",
		url.Values{"key": {"user.greeting"}, "value": {"hi"}, "version": {"1"}}, false)
	assertStatus(t, res, http.StatusConflict)

	cfg, _, err := b.GetServerConfig(t.Context())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"user.other": "1"}, cfg, "concurrent writer's config must survive")
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

// adminTestCertPEM generates a self-signed certificate PEM for form posts.
func adminTestCertPEM(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lxcon-server-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}, &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lxcon-server-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}, &key.PublicKey, key)
	require.NoError(t, err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestAddCertificateAppliesAndRedirects(t *testing.T) {
	b := fake.New()
	form := url.Values{"name": {"ci"}, "type": {"metrics"}, "certificate": {adminTestCertPEM(t)}}
	res := formRequest(t, New(b), "/server/certificates", form, false)
	assertStatus(t, res, http.StatusSeeOther)
	assert.Equal(t, "/server", res.Header().Get("Location"))

	certs, err := b.ListCertificates(t.Context())
	require.NoError(t, err)
	var found bool
	for _, c := range certs {
		if c.Name == "ci" && c.Type == "metrics" {
			found = true
		}
	}
	assert.True(t, found, "added certificate missing: %+v", certs)
}

func TestAddCertificateMissingFieldsIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/certificates",
		url.Values{"name": {""}, "type": {"client"}, "certificate": {""}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestAddCertificateBadTypeIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/certificates",
		url.Values{"name": {"x"}, "type": {"server"}, "certificate": {adminTestCertPEM(t)}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestAddCertificateBadPEMIs400(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/certificates",
		url.Values{"name": {"x"}, "type": {"client"}, "certificate": {"garbage"}}, false)
	assertStatus(t, res, http.StatusBadRequest)
}

func TestDeleteCertificateRemovesAndReturnsTable(t *testing.T) {
	b := fake.New()
	certs, err := b.ListCertificates(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, certs)
	fingerprint := certs[0].Fingerprint

	res := formRequest(t, New(b), "/server/certificates/"+fingerprint+"/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `id="certificates"`)

	after, err := b.ListCertificates(t.Context())
	require.NoError(t, err)
	for _, c := range after {
		assert.NotEqual(t, fingerprint, c.Fingerprint, "certificate must be gone after delete")
	}
}

func TestDeleteCertificateGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/certificates/ghost/delete", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestAckWarningFlipsStatusAndReturnsTable(t *testing.T) {
	b := fake.New()
	res := formRequest(t, New(b), "/server/warnings/fake-warning-1/ack", url.Values{}, true)
	assertStatus(t, res, http.StatusOK)
	assert.Contains(t, res.Body.String(), `id="warnings"`)

	warnings, err := b.ListWarnings(t.Context())
	require.NoError(t, err)
	for _, w := range warnings {
		if w.UUID == "fake-warning-1" {
			assert.Equal(t, "acknowledged", w.Status)
		}
	}
}

func TestAckWarningGhostIs404(t *testing.T) {
	res := formRequest(t, New(fake.New()), "/server/warnings/ghost/ack", url.Values{}, true)
	assertStatus(t, res, http.StatusNotFound)
}

func TestAddCertificateTooLargeIs413(t *testing.T) {
	form := url.Values{"name": {"big"}, "type": {"client"}, "certificate": {strings.Repeat("x", (64<<10)+1)}}
	res := formRequest(t, New(fake.New()), "/server/certificates", form, false)
	assertStatus(t, res, http.StatusRequestEntityTooLarge)
}
