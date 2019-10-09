package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
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

type TWorker struct {
	client *APIClient
	ctx    context.Context
	in     chan Tournament
	out    chan []string
}

func (w *TWorker) send(t Tournament) {
	w.in <- t
}

func (w *TWorker) receive() []string {
	return <-w.out
}

func (w *TWorker) process() {
	for item := range w.in {
		w.out <- w.getData(item)
	}
	close(w.out)
}

func (w *TWorker) getData(t Tournament) []string {
	resp, err := w.client.getTournamentPlayers(w.ctx, t.IDTournament, t.IDTeam)
	if err != nil {
		log.Println("Error getting players for tournament", err)
		return nil
	}

	var td []map[string]string
	err = json.Unmarshal(resp, &td)
	if err != nil {
		log.Println("Error parsing players for tournament", err)
		return nil
	}
	players := make([]string, len(td))
	for i, pl := range td {
		players[i] = pl["idplayer"]
	}
	return players
}

type PWorker struct {
	client *APIClient
	ctx    context.Context
	in     chan string
	out    chan *Player
}

func (w *PWorker) send(s string) {
	w.in <- s
}

func (w *PWorker) receive() *Player {
	return <-w.out
}

func (w PWorker) process() {
	for item := range w.in {
		// TODO: search in cache
		w.out <- w.getPlayerInfo(item)
	}
	close(w.out)
}

func (w PWorker) getPlayerInfo(pID string) *Player {
	resp, err := w.client.getPlayerInfo(w.ctx, pID)
	if err != nil {
		log.Println("Error getting player data", err)
		return nil
	}

	var pl []Player
	err = json.Unmarshal(resp, &pl)
	if err != nil {
		log.Println("Error parsing player data", err)
		return nil
	}
	return &pl[0]
}

// TODO: add cache
// TODO: add workers
// TODO: add routing
// TODO: add logging

type Tournament struct {
	IDTournament, IDTeam string
}

type Season struct {
	Tournaments []Tournament
}

type Server struct {
	*http.Server
	API *APIClient

	TWorkers []TWorker
	PWorkers []PWorker

	done chan bool
}

func NewServer(port int, router *http.ServeMux) *Server {
	s := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: router,
		// TODO: timeouts
		ReadTimeout:  20 * time.Second,
		WriteTimeout: 20 * time.Second,
	}
	return &Server{Server: s, API: NewAPIClient(), done: make(chan bool)}

}

func (s *Server) getPlayerTournaments(ctx context.Context, pID string) ([]Tournament, error) {
	resp, err := s.API.getPlayerTournaments(ctx, pID)
	if err != nil {
		log.Println("Error getting player tournaments", err)
		return nil, err
	}

	var seasons map[string]Season
	err = json.Unmarshal(resp, &seasons)
	if err != nil {
		log.Println("Error parsing tournaments response", err)
		return nil, err
	}

	var tournaments []Tournament
	for _, s := range seasons {
		tournaments = append(tournaments, s.Tournaments...)
	}
	return tournaments, nil
}

func (s *Server) getPlayerComrades(ctx context.Context, pID string, tournaments []Tournament) map[string]int {
	comrades := make(map[string]int)
	in := make(chan Tournament, 2)
	out := make(chan []string, 2)
	tworker := TWorker{client: s.API, ctx: ctx, in: in, out: out}
	go tworker.process()
	for _, t := range tournaments {
		tworker.send(t)
		for _, p := range tworker.receive() {
			// Skip current player
			if p == pID {
				continue
			}
			comrades[p]++
		}
	}
	close(in)
	return comrades
}

func (s *Server) getPlayerComradesInfo(ctx context.Context, comrades map[string]int) []Player {
	var players []Player
	in := make(chan string, 1)
	out := make(chan *Player, 1)
	pworker := PWorker{client: s.API, ctx: ctx, in: in, out: out}
	go pworker.process()

	for id, count := range comrades {
		pworker.send(id)
		pl := pworker.receive()
		if pl != nil {
			pl.Games = count
			players = append(players, *pl)
		}
	}
	close(in)
	return players
}

func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pID := vars["id"]
	ctx := r.Context()
	tournaments, err := s.getPlayerTournaments(ctx, pID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	comrades := s.getPlayerComrades(ctx, pID, tournaments)
	players := s.getPlayerComradesInfo(ctx, comrades)

	OrderBy(gamesSort, nameSort, surnameSort, patronymicSort).Sort(players)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(players)
}

func (s *Server) setupServerShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		// Wait for system exit signal
		<-quit
		// Allow main goroutine to finish
		defer close(s.done)

		log.Println("Shutting down the server")

		// TODO: allow to setup shutdown wait
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Disable ongoing keep-alive connections
		s.SetKeepAlivesEnabled(false)
		if err := s.Shutdown(ctx); err != nil {
			log.Fatalf("Could not shutdown the server: %s\n", err)
		}
	}()
}

func (s *Server) Start() {
	s.setupServerShutdown()

	err := s.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Fatalf("Unexpected server error: %s\n", err)
	}

	// Wait until shutdown is finished
	<-s.done
}

func main() {
	router := http.NewServeMux()
	r := mux.NewRouter()
	router.Handle("/", r)

	// TODO: read port from config
	server := NewServer(3000, router)

	r.HandleFunc("/comrades/{id:[0-9]+}", server.Handler)

	log.Println("Starting server")
	server.Start()
	log.Println("Server stopped")
}
