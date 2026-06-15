package powo

import (
	"context"
	"net/url"
	"strings"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes POWO as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/powo-cli/powo"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// powo:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone powo binary (see cli/root.go), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the POWO driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "powo",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "powo",
			Short:  "Read public plant taxon records from Plants of the World Online.",
			Long: `Read public plant taxon records from Plants of the World Online.

powo reads from the Kew Gardens POWO database (1.4M+ plant taxon records)
over plain HTTPS, shapes it into clean records, and prints output that pipes
into the rest of your tools. No API key required.`,
			Site: Host,
			Repo: "https://github.com/tamnd/powo-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	// search: search plant names.
	kit.Handle(app, kit.OpMeta{Name: "search", Group: "read", List: true,
		Summary: "Search plant names in the POWO database",
		Args:    []kit.Arg{{Name: "query", Help: "plant name or keyword (e.g. Poa, Rosa canina)"}}}, search)

	// taxon: get taxon detail by IPNI LSID.
	kit.Handle(app, kit.OpMeta{Name: "taxon", Group: "read", Single: true,
		Summary: "Get taxon detail by IPNI LSID", URIType: "taxon", Resolver: true,
		Args: []kit.Arg{{Name: "id", Help: "IPNI LSID (e.g. urn:lsid:ipni.org:names:30001404-2)"}}}, taxon)

	// recent: list recently listed taxa (empty query, sorted by name).
	kit.Handle(app, kit.OpMeta{Name: "recent", Group: "read", List: true,
		Summary: "List recently listed taxa"}, recent)
}

// newClient builds the POWO client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// --- inputs ---

type searchInput struct {
	Query  string  `kit:"arg"          help:"plant name or keyword"`
	Rank   string  `kit:"flag"         help:"filter by rank (e.g. Species, Genus, Family)"`
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

type taxonInput struct {
	ID     string  `kit:"arg"   help:"IPNI LSID (e.g. urn:lsid:ipni.org:names:30001404-2)"`
	Client *Client `kit:"inject"`
}

type recentInput struct {
	Limit  int     `kit:"flag,inherit" help:"max results"`
	Client *Client `kit:"inject"`
}

// --- handlers ---

func search(ctx context.Context, in searchInput, emit func(*Plant) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	plants, _, err := in.Client.SearchPlants(ctx, in.Query, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range plants {
		if in.Rank != "" && !strings.EqualFold(plants[i].Rank, in.Rank) {
			continue
		}
		if err := emit(&plants[i]); err != nil {
			return err
		}
	}
	return nil
}

func taxon(ctx context.Context, in taxonInput, emit func(*Plant) error) error {
	p, err := in.Client.GetPlant(ctx, in.ID)
	if err != nil {
		return mapErr(err)
	}
	return emit(p)
}

func recent(ctx context.Context, in recentInput, emit func(*Plant) error) error {
	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}
	plants, err := in.Client.RecentPlants(ctx, limit)
	if err != nil {
		return mapErr(err)
	}
	for i := range plants {
		if err := emit(&plants[i]); err != nil {
			return err
		}
	}
	return nil
}

// --- Resolver: pure string functions, no network ---

// Classify turns any accepted input into the canonical (type, id).
// IPNI LSIDs (starting with "urn:lsid:") are recognised directly; any
// other non-empty string is treated as a taxon reference too.
func (Domain) Classify(input string) (uriType, id string, err error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", "", errs.Usage("empty POWO reference")
	}
	// Both LSIDs and bare names resolve to "taxon".
	return "taxon", s, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	if uriType != "taxon" {
		return "", errs.Usage("powo has no resource type %q", uriType)
	}
	encoded := strings.ReplaceAll(url.PathEscape(id), "/", "%2F")
	return "https://" + Host + "/taxon/" + encoded, nil
}

// --- helpers ---

// mapErr converts a library error into the kit error kind that carries the
// right exit code.
func mapErr(err error) error {
	return err
}
