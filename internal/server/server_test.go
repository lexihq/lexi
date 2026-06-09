package server

import (
	"testing"

	"github.com/adam/lxcon/internal/backend/fake"
)

func TestServerSetsReadHeaderTimeout(t *testing.T) {
	srv := New(fake.New())

	if srv.ReadHeaderTimeout <= 0 {
		t.Fatalf("ReadHeaderTimeout = %v, want a positive timeout", srv.ReadHeaderTimeout)
	}
}
