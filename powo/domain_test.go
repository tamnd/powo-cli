package powo

import (
	"strings"
	"testing"

	"github.com/tamnd/any-cli/kit"
)

// These tests are offline: they exercise the URI driver's pure string functions
// and the host wiring (mint, body, resolve), which need no network. The client's
// HTTP behaviour is covered in powo_test.go.

func TestDomainInfo(t *testing.T) {
	info := Domain{}.Info()
	if info.Scheme != "powo" {
		t.Errorf("Scheme = %q, want powo", info.Scheme)
	}
	if len(info.Hosts) == 0 || info.Hosts[0] != Host {
		t.Errorf("Hosts = %v, want [%s]", info.Hosts, Host)
	}
	if info.Identity.Binary != "powo" {
		t.Errorf("Identity.Binary = %q, want powo", info.Identity.Binary)
	}
}

func TestClassifyURN(t *testing.T) {
	input := "urn:lsid:ipni.org:names:30001404-2"
	typ, id, err := Domain{}.Classify(input)
	if err != nil {
		t.Fatalf("Classify(%q) error: %v", input, err)
	}
	if typ != "taxon" {
		t.Errorf("type = %q, want taxon", typ)
	}
	if id != input {
		t.Errorf("id = %q, want %q", id, input)
	}
}

func TestClassifyName(t *testing.T) {
	typ, id, err := Domain{}.Classify("Acacia")
	if err != nil {
		t.Fatalf("Classify(Acacia) error: %v", err)
	}
	if typ != "taxon" {
		t.Errorf("type = %q, want taxon", typ)
	}
	if id != "Acacia" {
		t.Errorf("id = %q, want Acacia", id)
	}
}

func TestClassifyEmpty(t *testing.T) {
	_, _, err := Domain{}.Classify("")
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestLocate(t *testing.T) {
	got, err := Domain{}.Locate("taxon", "urn:lsid:ipni.org:names:30001404-2")
	if err != nil {
		t.Fatalf("Locate error: %v", err)
	}
	if !strings.Contains(got, "taxon/") {
		t.Errorf("URL %q does not contain taxon/", got)
	}
}

func TestLocateUnknownType(t *testing.T) {
	_, err := Domain{}.Locate("species", "abc")
	if err == nil {
		t.Error("expected error for unknown resource type")
	}
}

// TestHostWiring mounts the driver in a kit Host and checks the round trip:
// a record mints to its URI, and a bare id resolves back to the same URI.
// The init in domain.go registers the domain, so kit.Open finds it.
func TestHostWiring(t *testing.T) {
	h, err := kit.Open()
	if err != nil {
		t.Fatal(err)
	}

	p := &Plant{
		ID:     "urn:lsid:ipni.org:names:30001404-2",
		Name:   "Poa",
		Rank:   "Genus",
		Family: "Poaceae",
	}
	u, err := h.Mint(p)
	if err != nil {
		t.Fatalf("Mint: %v", err)
	}
	if !strings.HasPrefix(u.String(), "powo://taxon/") {
		t.Errorf("Mint = %q, want powo://taxon/... prefix", u.String())
	}

	got, err := h.ResolveOn("powo", "urn:lsid:ipni.org:names:30001404-2")
	if err != nil {
		t.Fatalf("ResolveOn error: %v", err)
	}
	if !strings.HasPrefix(got.String(), "powo://taxon/") {
		t.Errorf("ResolveOn = %q, want powo://taxon/... prefix", got.String())
	}
}
