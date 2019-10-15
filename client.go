package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"time"
)

type APIClient struct {
	BaseURL   *url.URL
	UserAgent string

	httpClient *http.Client
}

type Option func(*APIClient)

func UserAgent(ua string) Option {
	return func(c *APIClient) {
		c.UserAgent = ua
	}
}

func BaseUrl(url *url.URL) Option {
	return func(c *APIClient) {
		c.BaseURL = url
	}
}

func Timeout(t time.Duration) Option {
	return func(c *APIClient) {
		c.httpClient.Timeout = t
	}
}

func NewAPIClient(options ...Option) *APIClient {
	// TODO: add options
	c := &http.Client{
		Timeout: 10 * time.Second,
	}

	api := &APIClient{httpClient: c}

	for _, opt := range options {
		opt(api)
	}

	if api.BaseURL == nil {
		url, err := url.Parse("https://rating.chgk.info/")
		if err != nil {
			log.Fatal("Invalid base url")
		}
		api.BaseURL = url
	}

	return api
}

func (api APIClient) getURL(path string, items ...interface{}) *url.URL {
	rel := &url.URL{Path: fmt.Sprintf(path, items...)}
	return api.BaseURL.ResolveReference(rel)
}

func (api APIClient) prepareRequest(ctx context.Context, method string, url *url.URL) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url.String(), nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", api.UserAgent)
	return req, nil
}

func (api APIClient) do(req *http.Request) (*http.Response, error) {
	resp, err := api.httpClient.Do(req)
	if err != nil {
		log.Printf("--> %s %s Error: %s", req.Method, req.URL.Path, err)
		return nil, err
	}

	log.Printf("--> %s %s %d", req.Method, req.URL.Path, resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Response code %d", resp.StatusCode)
	}
	return resp, nil
}

func (api APIClient) ListPlayerTournaments(ctx context.Context, pID string) ([]Tournament, error) {
	url := api.getURL("api/players/%s/tournaments.json", pID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := api.prepareRequest(ctx, http.MethodGet, url)
	if err != nil {
		log.Println("Error creating player tournaments request", err)
		return nil, err
	}

	resp, err := api.do(req)
	if err != nil {
		log.Println("Error retrieving player tournaments", err)
		return nil, err
	}
	defer resp.Body.Close()

	var seasons map[string]Season
	err = json.NewDecoder(resp.Body).Decode(&seasons)
	if err != nil {
		return nil, err
	}

	var tournaments []Tournament
	for _, s := range seasons {
		tournaments = append(tournaments, s.Tournaments...)
	}
	return tournaments, err
}

func (api APIClient) ListTournamentPlayers(ctx context.Context, tID, teamID string) ([]string, error) {
	url := api.getURL("api/tournaments/%s/recaps/%s.json", tID, teamID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := api.prepareRequest(ctx, http.MethodGet, url)
	if err != nil {
		log.Println("Error creating tournaments players request", err)
		return nil, err
	}

	resp, err := api.do(req)
	if err != nil {
		log.Println("Error retrieving players for tournament", err)
		return nil, err
	}
	defer resp.Body.Close()

	var td []map[string]string
	err = json.NewDecoder(resp.Body).Decode(&td)
	if err != nil {
		return nil, err
	}

	players := make([]string, len(td))
	for i, pl := range td {
		players[i] = pl["idplayer"]
	}
	return players, nil
}

func (api APIClient) PlayerInfo(ctx context.Context, pID string) (*Player, error) {
	url := api.getURL("api/players/%s.json", pID)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	req, err := api.prepareRequest(ctx, http.MethodGet, url)
	if err != nil {
		log.Println("Error creating player info request", err)
		return nil, err
	}

	resp, err := api.do(req)
	if err != nil {
		log.Println("Error retrieving player info", err)
		return nil, err
	}

	var pl []Player
	err = json.NewDecoder(resp.Body).Decode(&pl)
	if err != nil {
		log.Println("Error parsing player data", err)
		return nil, err
	}
	return &pl[0], nil
}
