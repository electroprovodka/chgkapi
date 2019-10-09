package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/gorilla/mux"

	"github.com/go-redis/redis"
)

type TWorkerPayload struct {
	ctx context.Context
	wg  *sync.WaitGroup
	in  chan Tournament
	out chan []string
}

type TWorker struct {
	client *APIClient
	in     chan *TWorkerPayload
}

func (w *TWorker) process() {
	for p := range w.in {
		for item := range p.in {
			p.out <- w.getData(p.ctx, item)
		}
		p.wg.Done()
	}
}

func (w *TWorker) getData(ctx context.Context, t Tournament) []string {
	resp, err := w.client.getTournamentPlayers(ctx, t.IDTournament, t.IDTeam)
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

type TWorkerPool struct {
	nworkers int
	workers  []TWorker

	in chan *TWorkerPayload
}

func setupTWorkerPool(nworkers int, c *APIClient) TWorkerPool {
	in := make(chan *TWorkerPayload, nworkers)
	workers := make([]TWorker, nworkers)
	for i := 0; i < nworkers; i++ {
		workers[i] = TWorker{c, in}
		go workers[i].process()
	}

	return TWorkerPool{nworkers, workers, in}
}

func (twp *TWorkerPool) send(ctx context.Context, tt []Tournament) <-chan []string {
	in := make(chan Tournament, twp.nworkers)
	out := make(chan []string, twp.nworkers)
	wg := &sync.WaitGroup{}
	// Run task on multiple workers
	for i := 0; i < twp.nworkers; i++ {
		wg.Add(1)
		twp.in <- &TWorkerPayload{ctx, wg, in, out}
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	go func() {
		for _, t := range tt {
			in <- t
		}
		close(in)
	}()

	return out
}

type PWorkerPayload struct {
	ctx context.Context
	wg  *sync.WaitGroup
	in  chan string
	out chan *Player
}

type PWorker struct {
	client *APIClient
	in     chan *PWorkerPayload
}

func (w PWorker) process() {
	for p := range w.in {
		for item := range p.in {
			// TODO: search in cache
			p.out <- w.getPlayerInfo(p.ctx, item)
		}
		p.wg.Done()
	}

}

func (w PWorker) getPlayerInfo(ctx context.Context, pID string) *Player {
	resp, err := w.client.getPlayerInfo(ctx, pID)
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

type PWorkerPool struct {
	nworkers int
	workers  []PWorker

	in chan *PWorkerPayload
}

func setupPWorkerPool(nworkers int, c *APIClient) PWorkerPool {
	in := make(chan *PWorkerPayload, nworkers)
	workers := make([]PWorker, nworkers)
	for i := 0; i < nworkers; i++ {
		workers[i] = PWorker{c, in}
		go workers[i].process()
	}

	return PWorkerPool{nworkers, workers, in}
}

func (pwp *PWorkerPool) send(ctx context.Context, players []string) <-chan *Player {
	in := make(chan string, pwp.nworkers)
	out := make(chan *Player, pwp.nworkers)
	wg := &sync.WaitGroup{}
	// Run task on multiple workers
	for i := 0; i < pwp.nworkers; i++ {
		wg.Add(1)
		pwp.in <- &PWorkerPayload{ctx, wg, in, out}
	}

	go func() {
		wg.Wait()
		close(out)
	}()

	go func() {
		for _, p := range players {
			in <- p
		}
		close(in)
	}()

	return out
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

	rcl *redis.Client

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
	rcl := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		Password: "",
		DB:       0,
	})
	_, err := rcl.Ping().Result()
	if err != nil {
		log.Fatal(err)
	}
	return &Server{Server: s, API: NewAPIClient(), rcl: rcl, done: make(chan bool)}

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
	wp := setupTWorkerPool(4, s.API)
	comrades := make(map[string]int)
	out := wp.send(ctx, tournaments)
	for pl := range out {
		for _, p := range pl {
			// Skip current player
			if p == pID {
				continue
			}
			comrades[p]++
		}
	}
	return comrades
}

func (s *Server) getPlayerComradesInfo(ctx context.Context, comrades map[string]int) []Player {
	wp := setupPWorkerPool(4, s.API)
	var pp []string
	var players []Player

	for id := range comrades {
		val, err := s.rcl.Get(id).Bytes()
		if err != nil {
			pp = append(pp, id)
		} else {
			log.Println("Hit for player", id)
			var pl Player
			err := json.Unmarshal(val, &pl)
			if err != nil {
				log.Println(err)
				continue
			}
			players = append(players, pl)
		}
	}
	out := wp.send(ctx, pp)

	for pl := range out {
		if pl != nil {
			pl.Games = comrades[pl.IDplayer]
			players = append(players, *pl)
			bt, err := json.Marshal(pl)
			if err != nil {
				log.Println(err)
				continue
			}
			err = s.rcl.Set(pl.IDplayer, bt, time.Hour).Err()
			if err != nil {
				log.Println(err)
				continue
			}
		}
	}
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
