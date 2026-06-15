package powo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchPlants(t *testing.T) {
	const resp = `{
		"totalResults": 1449552,
		"cursor": "abc",
		"results": [
			{
				"fqId": "urn:lsid:ipni.org:names:30001404-2",
				"name": "Poa",
				"rank": "Genus",
				"accepted": true,
				"author": "L.",
				"kingdom": "Plantae",
				"family": "Poaceae",
				"snippet": "A genus of grasses."
			},
			{
				"fqId": "urn:lsid:ipni.org:names:12345-1",
				"name": "Rosa canina",
				"rank": "Species",
				"accepted": false,
				"author": "L.",
				"kingdom": "Plantae",
				"family": "Rosaceae",
				"snippet": "Dog rose."
			}
		]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	plants, total, err := c.searchAt(context.Background(), srv.URL+"?q=Poa&perPage=20")
	if err != nil {
		t.Fatal(err)
	}
	if total != 1449552 {
		t.Errorf("total = %d, want 1449552", total)
	}
	if len(plants) != 2 {
		t.Fatalf("len(plants) = %d, want 2", len(plants))
	}
	if plants[0].Name != "Poa" {
		t.Errorf("Name = %q, want Poa", plants[0].Name)
	}
	if plants[0].Status != "Accepted" {
		t.Errorf("Status = %q, want Accepted", plants[0].Status)
	}
	if plants[1].Status != "Synonym" {
		t.Errorf("Status = %q, want Synonym", plants[1].Status)
	}
}

func TestGetPlant(t *testing.T) {
	const resp = `{
		"fqId": "urn:lsid:ipni.org:names:30001404-2",
		"name": "Poa",
		"genus": "Poa",
		"family": "Poaceae",
		"kingdom": "Plantae",
		"rank": "Genus",
		"taxonomicStatus": "Accepted",
		"namePublishedInYear": 1753,
		"authors": "L.",
		"synonym": false,
		"lifeform": ["annual", "perennial"],
		"climate": ["temperate"]
	}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	plant, err := c.getPlantAt(context.Background(), srv.URL+"/taxon/urn%3Alsid%3Aipni.org%3Anames%3A30001404-2")
	if err != nil {
		t.Fatal(err)
	}
	if plant.Status != "Accepted" {
		t.Errorf("Status = %q, want Accepted", plant.Status)
	}
	if plant.Author != "L." {
		t.Errorf("Author = %q, want L. (mapped from authors)", plant.Author)
	}
	if plant.PublishedYear != 1753 {
		t.Errorf("PublishedYear = %d, want 1753", plant.PublishedYear)
	}
	if len(plant.Lifeforms) != 2 {
		t.Errorf("Lifeforms = %v, want 2 items", plant.Lifeforms)
	}
}

func TestRecentPlants(t *testing.T) {
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalResults":1449552,"cursor":"","results":[]}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0

	_, err := c.recentAt(context.Background(), srv.URL+"?q=&perPage=10")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "perPage=10") {
		t.Errorf("URL %q does not contain perPage=10", gotURL)
	}
}

func TestRetryOn503(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"totalResults":1,"cursor":"","results":[{"fqId":"x","name":"Test","rank":"Species","accepted":true,"author":"","kingdom":"Plantae","family":"Testaceae"}]}`))
	}))
	defer srv.Close()

	c := NewClient()
	c.Rate = 0
	c.Retries = 5

	start := time.Now()
	plants, _, err := c.searchAt(context.Background(), srv.URL+"?q=Test&perPage=1")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 3 {
		t.Errorf("server saw %d hits, want 3", hits)
	}
	if len(plants) != 1 {
		t.Errorf("len(plants) = %d, want 1", len(plants))
	}
	if time.Since(start) < 500*time.Millisecond {
		t.Error("retries did not back off")
	}
}
