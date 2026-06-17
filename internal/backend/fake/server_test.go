package fake

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"math/big"
	"slices"
	"testing"
	"time"

	"github.com/lexihq/lexi/internal/backend"
)

func TestGetServerOverviewStatic(t *testing.T) {
	o, err := New().GetServerOverview(ctx())
	if err != nil {
		t.Fatalf("overview: %v", err)
	}
	if o.ServerVersion == "" || o.CPUThreads == 0 || o.MemoryTotal == 0 {
		t.Fatalf("overview missing fields: %+v", o)
	}
}

func TestGetServerHardwareStatic(t *testing.T) {
	hw, err := New().GetServerHardware(ctx())
	if err != nil {
		t.Fatalf("hardware: %v", err)
	}
	if len(hw.GPUs) == 0 || len(hw.NICs) == 0 || len(hw.Disks) == 0 {
		t.Fatalf("hardware missing devices: %+v", hw)
	}
	if gpu := hw.GPUs[0]; gpu.Vendor == "" || gpu.Product == "" || gpu.Driver == "" {
		t.Errorf("gpu missing fields: %+v", gpu)
	}
	nic := hw.NICs[0]
	if nic.Product == "" || len(nic.Ports) == 0 || nic.Ports[0].ID == "" || nic.Ports[0].Address == "" {
		t.Errorf("nic missing fields: %+v", nic)
	}
	if disk := hw.Disks[0]; disk.ID == "" || disk.Model == "" || disk.SizeBytes == 0 {
		t.Errorf("disk missing fields: %+v", disk)
	}
}

func TestServerConfigRoundTrip(t *testing.T) {
	b := New()
	cfg, version, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["core.https_address"] == "" {
		t.Fatalf("expected seeded core.https_address, got %+v", cfg)
	}
	if version == "" {
		t.Fatal("expected a non-empty config version token")
	}

	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.x": "1"}, version); err != nil {
		t.Fatalf("update config: %v", err)
	}
	cfg, _, err = b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["user.x"] != "1" {
		t.Errorf("updated key missing: %+v", cfg)
	}
	if _, ok := cfg["core.https_address"]; ok {
		t.Errorf("replace semantics: dropped key survived: %+v", cfg)
	}
}

func TestServerConfigStaleVersionIsConflict(t *testing.T) {
	b := New()
	_, version, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	// A concurrent writer lands first; the held version goes stale.
	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.first": "1"}, version); err != nil {
		t.Fatalf("first update: %v", err)
	}
	err = b.UpdateServerConfig(ctx(), map[string]string{"user.second": "2"}, version)
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("stale version must conflict, got %v", err)
	}
	cfg, _, err := b.GetServerConfig(ctx())
	if err != nil {
		t.Fatalf("get config: %v", err)
	}
	if cfg["user.first"] != "1" {
		t.Errorf("first writer's config must survive: %+v", cfg)
	}
}

func TestServerConfigEmptyVersionIsUnconditional(t *testing.T) {
	// Mirrors the Incus client: an empty etag sends no If-Match.
	b := New()
	if err := b.UpdateServerConfig(ctx(), map[string]string{"user.x": "1"}, ""); err != nil {
		t.Fatalf("unconditional update: %v", err)
	}
}

func TestListCertificatesSeeded(t *testing.T) {
	certs, err := New().ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list certificates: %v", err)
	}
	if len(certs) == 0 || certs[0].Fingerprint == "" {
		t.Fatalf("expected a seeded certificate, got %+v", certs)
	}
}

func TestWarningsSeededAndDelete(t *testing.T) {
	b := New()
	warnings, err := b.ListWarnings(ctx())
	if err != nil {
		t.Fatalf("list warnings: %v", err)
	}
	if len(warnings) < 2 {
		t.Fatalf("expected seeded warnings, got %+v", warnings)
	}
	if warnings[0].LastSeenAt.Before(warnings[1].LastSeenAt) {
		t.Errorf("warnings must sort newest first: %+v", warnings)
	}

	if err := b.DeleteWarning(ctx(), warnings[0].UUID); err != nil {
		t.Fatalf("delete warning: %v", err)
	}
	after, err := b.ListWarnings(ctx())
	if err != nil {
		t.Fatalf("list warnings: %v", err)
	}
	if len(after) != len(warnings)-1 {
		t.Fatalf("expected %d warnings after delete, got %d", len(warnings)-1, len(after))
	}
}

func TestDeleteWarningGhostIs404(t *testing.T) {
	err := New().DeleteWarning(ctx(), "no-such-uuid")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

// testCertPEM generates a self-signed certificate and returns its PEM encoding
// plus the sha256 fingerprint of the DER bytes.
func testCertPEM(t *testing.T) (string, string) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lexi-test"},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	sum := sha256.Sum256(der)
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})), hex.EncodeToString(sum[:])
}

func TestAddCertificateStoresParsedEntry(t *testing.T) {
	f := New()
	pemData, fingerprint := testCertPEM(t)

	if err := f.AddCertificate(ctx(), "ci-runner", "client", pemData); err != nil {
		t.Fatalf("add certificate: %v", err)
	}

	certs, err := f.ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *backend.Certificate
	for i := range certs {
		if certs[i].Name == "ci-runner" {
			found = &certs[i]
		}
	}
	if found == nil || found.Type != "client" || found.Fingerprint != fingerprint {
		t.Fatalf("stored cert mismatch: %+v (want fingerprint %s)", found, fingerprint)
	}
}

func TestAddCertificateBadPEMIsInvalid(t *testing.T) {
	f := New()
	err := f.AddCertificate(ctx(), "junk", "client", "not a pem")
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestAddCertificateDuplicateIsConflict(t *testing.T) {
	f := New()
	pemData, _ := testCertPEM(t)
	if err := f.AddCertificate(ctx(), "one", "client", pemData); err != nil {
		t.Fatalf("first add: %v", err)
	}
	err := f.AddCertificate(ctx(), "two", "client", pemData)
	if !errors.Is(err, backend.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
}

func TestDeleteCertificateRemovesByFingerprint(t *testing.T) {
	f := New()
	pemData, fingerprint := testCertPEM(t)
	if err := f.AddCertificate(ctx(), "ci-runner", "client", pemData); err != nil {
		t.Fatalf("add certificate: %v", err)
	}
	if err := f.DeleteCertificate(ctx(), fingerprint); err != nil {
		t.Fatalf("delete certificate: %v", err)
	}
	certs, err := f.ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range certs {
		if c.Fingerprint == fingerprint {
			t.Fatalf("certificate %s still present after delete", fingerprint)
		}
	}
}

func TestDeleteCertificateGhostIs404(t *testing.T) {
	err := New().DeleteCertificate(ctx(), "no-such-fingerprint")
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestUpdateCertificateRenamesAndRestricts(t *testing.T) {
	f := New()
	pemData, fingerprint := testCertPEM(t)
	if err := f.AddCertificate(ctx(), "ci-runner", "client", pemData); err != nil {
		t.Fatalf("add certificate: %v", err)
	}

	if err := f.UpdateCertificate(ctx(), fingerprint, "ci-runner-2", true, []string{"default", "dev"}); err != nil {
		t.Fatalf("update certificate: %v", err)
	}

	certs, err := f.ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var found *backend.Certificate
	for i := range certs {
		if certs[i].Fingerprint == fingerprint {
			found = &certs[i]
		}
	}
	if found == nil {
		t.Fatalf("certificate %s missing after update", fingerprint)
	}
	if found.Name != "ci-runner-2" || !found.Restricted || !slices.Equal(found.Projects, []string{"default", "dev"}) {
		t.Fatalf("updated cert mismatch: %+v", found)
	}
}

func TestUpdateCertificateUnrestrictClearsProjects(t *testing.T) {
	f := New()
	pemData, fingerprint := testCertPEM(t)
	if err := f.AddCertificate(ctx(), "ci-runner", "client", pemData); err != nil {
		t.Fatalf("add certificate: %v", err)
	}
	if err := f.UpdateCertificate(ctx(), fingerprint, "ci-runner", true, []string{"default"}); err != nil {
		t.Fatalf("restrict: %v", err)
	}

	if err := f.UpdateCertificate(ctx(), fingerprint, "ci-runner", false, nil); err != nil {
		t.Fatalf("unrestrict: %v", err)
	}

	certs, err := f.ListCertificates(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, c := range certs {
		if c.Fingerprint == fingerprint {
			if c.Restricted || len(c.Projects) != 0 {
				t.Fatalf("want unrestricted with no projects, got %+v", c)
			}
			return
		}
	}
	t.Fatalf("certificate %s missing after update", fingerprint)
}

func TestUpdateCertificateEmptyNameIsInvalid(t *testing.T) {
	f := New()
	pemData, fingerprint := testCertPEM(t)
	if err := f.AddCertificate(ctx(), "ci-runner", "client", pemData); err != nil {
		t.Fatalf("add certificate: %v", err)
	}
	err := f.UpdateCertificate(ctx(), fingerprint, "", false, nil)
	if !errors.Is(err, backend.ErrInvalid) {
		t.Fatalf("want ErrInvalid, got %v", err)
	}
}

func TestUpdateCertificateGhostIs404(t *testing.T) {
	err := New().UpdateCertificate(ctx(), "no-such-fingerprint", "name", false, nil)
	if !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestAcknowledgeWarningFlipsStatus(t *testing.T) {
	f := New()
	if err := f.AcknowledgeWarning(ctx(), "fake-warning-1"); err != nil {
		t.Fatalf("acknowledge: %v", err)
	}
	warnings, err := f.ListWarnings(ctx())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, w := range warnings {
		if w.UUID == "fake-warning-1" {
			if w.Status != "acknowledged" {
				t.Fatalf("status = %q, want acknowledged", w.Status)
			}
			return
		}
	}
	t.Fatalf("warning fake-warning-1 missing: %+v", warnings)
}

func TestAcknowledgeWarningGhostIs404(t *testing.T) {
	f := New()
	if err := f.AcknowledgeWarning(ctx(), "ghost"); !errors.Is(err, backend.ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}
