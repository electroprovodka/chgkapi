package main

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"time"
)

type APIClient struct {
	http.Client
	BaseURL string
}

func NewAPIClient() *APIClient {
	// TODO: add options
	c := http.Client{
		Timeout: 10 * time.Second,
	}
	return &APIClient{c, "https://rating.chgk.info/api"}
}

func (api APIClient) getURL(path string, items ...interface{}) string {
	return api.BaseURL + fmt.Sprintf(path, items...)
}

func (api APIClient) prepareRequest(ctx context.Context, method, url string) (*http.Request, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req = req.WithContext(ctx)
	return req, nil
}

func (api APIClient) do(req *http.Request) (*http.Response, error) {
	resp, err := api.Do(req)
	if err != nil {
		log.Printf("--> %s %s Error: %s", req.Method, req.URL.Path, err)
	} else {
		log.Printf("--> %s %s %d", req.Method, req.URL.Path, resp.StatusCode)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Response code %d", resp.StatusCode)
	}
	return resp, err
}

func (api APIClient) getPlayerTournaments(ctx context.Context, pID string) ([]byte, error) {
	url := api.getURL("/players/%s/tournaments.json", pID)
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

	tb, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading tournaments response", err)
		return nil, err
	}
	return tb, nil
}

func (api APIClient) getTournamentPlayers(ctx context.Context, tID, teamID string) ([]byte, error) {
	url := api.getURL("/tournaments/%s/recaps/%s.json", tID, teamID)
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

	pb, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading players for tournament", err)
		return nil, err
	}
	return pb, nil
}

func (api APIClient) getPlayerInfo(ctx context.Context, pID string) ([]byte, error) {
	url := api.getURL("/players/%s.json", pID)
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

	pb, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading player info", err)
		return nil, err
	}
	return pb, nil
}
