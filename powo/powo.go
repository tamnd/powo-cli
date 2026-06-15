// Package powo is the library behind the powo command line:
// the HTTP client, request shaping, and the typed data models for the
// Plants of the World Online (POWO) database from Kew Gardens.
//
// The Client here is the spine every command shares. It sets a real
// User-Agent, paces requests so a busy session stays polite, and retries the
// transient failures (429 and 5xx) that any public API throws under load.
// Build your endpoint calls and JSON decoding on top of it.
package powo

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultUserAgent identifies the client to POWO.
const DefaultUserAgent = "powo-cli/0.1.0"

// Host is the POWO site this client talks to.
const Host = "powo.science.kew.org"

// BaseURL is the root every request is built from.
const BaseURL = "https://" + Host + "/api/2"

// --- wire types (unexported) ---

type wireSearchResult struct {
	FqID     string `json:"fqId"`
	Name     string `json:"name"`
	Rank     string `json:"rank"`
	Accepted bool   `json:"accepted"`
	Author   string `json:"author"`
	Kingdom  string `json:"kingdom"`
	Family   string `json:"family"`
	Snippet  string `json:"snippet"`
}

type wireSearchResp struct {
	TotalResults int                `json:"totalResults"`
	Cursor       string             `json:"cursor"`
	Results      []wireSearchResult `json:"results"`
}

type wireDetail struct {
	FqID                string   `json:"fqId"`
	Name                string   `json:"name"`
	Genus               string   `json:"genus"`
	Family              string   `json:"family"`
	Kingdom             string   `json:"kingdom"`
	Rank                string   `json:"rank"`
	TaxonomicStatus     string   `json:"taxonomicStatus"`
	NamePublishedInYear int      `json:"namePublishedInYear"`
	Authors             string   `json:"authors"`
	Synonym             bool     `json:"synonym"`
	Lifeform            []string `json:"lifeform"`
	Climate             []string `json:"climate"`
}

// --- public output types ---

// Plant is one taxon record from the POWO database.
type Plant struct {
	ID            string   `json:"id"             kit:"id"` // fqId
	Name          string   `json:"name"`
	Rank          string   `json:"rank"`
	Accepted      bool     `json:"accepted"`
	Author        string   `json:"author"`
	Kingdom       string   `json:"kingdom"`
	Family        string   `json:"family"`
	Status        string   `json:"status"`         // taxonomicStatus from detail or "Accepted"/"Synonym" from search
	PublishedYear int      `json:"published_year"`
	Lifeforms     []string `json:"lifeforms"`
	Climates      []string `json:"climates"`
}

// Client talks to POWO over HTTP.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: a 30s timeout, a 300ms
// minimum gap between requests, and three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		Rate:      300 * time.Millisecond,
		Retries:   3,
	}
}

// SearchPlants queries /search and returns matching plants plus the total count.
func (c *Client) SearchPlants(ctx context.Context, query string, limit int) ([]Plant, int, error) {
	if limit <= 0 {
		limit = 20
	}
	rawURL := BaseURL + "/search?q=" + url.QueryEscape(query) + "&perPage=" + strconv.Itoa(limit)
	return c.searchAt(ctx, rawURL)
}

// searchAt is the testable core of SearchPlants; tests point it at an httptest server.
func (c *Client) searchAt(ctx context.Context, rawURL string) ([]Plant, int, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, 0, err
	}
	return parseSearchResp(body)
}

// GetPlant fetches a single taxon by its IPNI LSID fqId
// (e.g. "urn:lsid:ipni.org:names:30001404-2").
func (c *Client) GetPlant(ctx context.Context, fqID string) (*Plant, error) {
	// fqId contains ":" and "/" which must be percent-encoded in the path segment.
	// url.QueryEscape encodes both, matching the expected form:
	// /taxon/urn%3Alsid%3Aipni.org%3Anames%3A30001404-2
	encoded := strings.ReplaceAll(url.PathEscape(fqID), "/", "%2F")
	rawURL := BaseURL + "/taxon/" + encoded
	return c.getPlantAt(ctx, rawURL)
}

// getPlantAt is the testable core of GetPlant; tests point it at an httptest server.
func (c *Client) getPlantAt(ctx context.Context, rawURL string) (*Plant, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	return parseDetailResp(body)
}

// RecentPlants returns the most recently listed plants by querying with an empty
// query (which returns all 1.4M records sorted by name) and limiting the result.
func (c *Client) RecentPlants(ctx context.Context, limit int) ([]Plant, error) {
	if limit <= 0 {
		limit = 20
	}
	rawURL := BaseURL + "/search?q=&perPage=" + strconv.Itoa(limit)
	return c.recentAt(ctx, rawURL)
}

// recentAt is the testable core of RecentPlants; tests point it at an httptest server.
func (c *Client) recentAt(ctx context.Context, rawURL string) ([]Plant, error) {
	body, err := c.get(ctx, rawURL)
	if err != nil {
		return nil, err
	}
	plants, _, err := parseSearchResp(body)
	return plants, err
}

// get fetches a URL and returns the response body, with pacing and retries.
func (c *Client) get(ctx context.Context, rawURL string) ([]byte, error) {
	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}
		body, retry, err := c.do(ctx, rawURL)
		if err == nil {
			return body, nil
		}
		lastErr = err
		if !retry {
			return nil, err
		}
	}
	return nil, fmt.Errorf("get %s: %w", rawURL, lastErr)
}

func (c *Client) do(ctx context.Context, rawURL string) (body []byte, retry bool, err error) {
	c.pace()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, true, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf("http %d", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("http %d", resp.StatusCode)
	}

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, err
	}
	return b, false, nil
}

// pace blocks until at least Rate has passed since the previous request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

// --- parse helpers ---

func parseSearchResp(body []byte) ([]Plant, int, error) {
	var resp wireSearchResp
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, 0, fmt.Errorf("decode search response: %w", err)
	}
	plants := make([]Plant, 0, len(resp.Results))
	for _, r := range resp.Results {
		plants = append(plants, plantFromSearch(r))
	}
	return plants, resp.TotalResults, nil
}

func parseDetailResp(body []byte) (*Plant, error) {
	var d wireDetail
	if err := json.Unmarshal(body, &d); err != nil {
		return nil, fmt.Errorf("decode taxon response: %w", err)
	}
	p := plantFromDetail(d)
	return p, nil
}

func plantFromSearch(r wireSearchResult) Plant {
	status := "Synonym"
	if r.Accepted {
		status = "Accepted"
	}
	return Plant{
		ID:       r.FqID,
		Name:     r.Name,
		Rank:     r.Rank,
		Accepted: r.Accepted,
		Author:   r.Author,
		Kingdom:  r.Kingdom,
		Family:   r.Family,
		Status:   status,
	}
}

func plantFromDetail(d wireDetail) *Plant {
	accepted := !d.Synonym
	return &Plant{
		ID:            d.FqID,
		Name:          d.Name,
		Rank:          d.Rank,
		Accepted:      accepted,
		Author:        d.Authors,
		Kingdom:       d.Kingdom,
		Family:        d.Family,
		Status:        d.TaxonomicStatus,
		PublishedYear: d.NamePublishedInYear,
		Lifeforms:     d.Lifeform,
		Climates:      d.Climate,
	}
}
