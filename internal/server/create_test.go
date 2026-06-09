package server

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/adam/lxcon/internal/backend/fake"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCreateValidatesNameAndImage(t *testing.T) {
	tests := []struct {
		name string
		form url.Values
		want string
	}{
		{name: "missing name", form: url.Values{"image": {"debian/12"}}, want: "name is required"},
		{name: "missing image", form: url.Values{"name": {"demo"}}, want: "image is required"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := formRequest(t, New(fake.New()), "/instances", tt.form, true)

			assertStatus(t, res, http.StatusBadRequest)
			if body := res.Body.String(); !strings.Contains(body, tt.want) {
				t.Fatalf("expected %q in response, got %q", tt.want, body)
			}
		})
	}
}

func TestCreateHXReturnsCreatedRow(t *testing.T) {
	b := fake.New()
	form := url.Values{"name": {"demo"}, "image": {"fake-debian-12-aarch64"}, "start": {"on"}}

	res := formRequest(t, New(b), "/instances", form, true)

	assertStatus(t, res, http.StatusOK)
	body := res.Body.String()
	if !strings.Contains(body, "demo") || !strings.Contains(body, "Running") {
		t.Fatalf("expected created running row, got %q", body)
	}
	inst, err := b.GetInstance(t.Context(), "demo")
	require.NoError(t, err)
	assert.Equal(t, "debian/12", inst.Image)
}

func TestCreateRejectsUnknownImageFingerprint(t *testing.T) {
	form := url.Values{"name": {"demo"}, "image": {"unknown-fingerprint"}}

	res := formRequest(t, New(fake.New()), "/instances", form, true)

	assert.Equal(t, http.StatusBadRequest, res.Code)
	assert.Contains(t, res.Body.String(), "selected image is unavailable")
}

func TestImagesFilter(t *testing.T) {
	srv := New(fake.New())

	t.Run("by query matches distribution", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?q=debian", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "debian/12")
		assert.NotContains(t, body, "fedora/40")
		assert.NotContains(t, body, "alpine/edge")
	})

	t.Run("by arch", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?arch=x86_64", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "fedora/40")
		assert.Contains(t, body, "debian/12")
		assert.Contains(t, body, "ubuntu/24.04")
		assert.NotContains(t, body, "alpine/edge")
	})

	t.Run("by type", func(t *testing.T) {
		res := request(t, srv, "GET", "/images?type=virtual-machine", "", true)
		assert.Equal(t, http.StatusOK, res.Code)
		body := res.Body.String()
		assert.Contains(t, body, "ubuntu/24.04")
		assert.Contains(t, body, "virtual-machine")
		assert.NotContains(t, body, "debian/12")
	})
}
