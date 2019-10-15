package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
)

type TWorkerPayload struct {
	ctx context.Context
	in  chan Tournament
	out chan []string
}

type TWorker struct {
	client *APIClient
	in     chan *TWorkerPayload
}

func (w *TWorker) process() {
	for p := range w.in {
	processLoop:
		for item := range p.in {
			select {
			case <-p.ctx.Done():
				break processLoop
			case p.out <- w.getPlayersIDs(p.ctx, item):
				break
			}
		}
		close(p.out)
	}
}

func (w *TWorker) getPlayersIDs(ctx context.Context, t Tournament) []string {
	players, err := w.client.ListTournamentPlayers(ctx, t.IDTournament, t.IDTeam)
	if err != nil {
		log.Println("Error getting players for tournament", err)
		return nil
	}
	return players
}

type TWorkerPool struct {
	nworkers int
	workers  []TWorker

	in chan *TWorkerPayload
}

func setupTWorkerPool(nworkers int, c *APIClient) *TWorkerPool {
	in := make(chan *TWorkerPayload, nworkers)
	workers := make([]TWorker, nworkers)
	for i := 0; i < nworkers; i++ {
		workers[i] = TWorker{c, in}
		go workers[i].process()
	}

	return &TWorkerPool{nworkers, workers, in}
}

func (twp *TWorkerPool) send(ctx context.Context, tt []Tournament) <-chan []string {
	in := make(chan Tournament, twp.nworkers)
	out := make(chan []string, twp.nworkers)
	twp.in <- &TWorkerPayload{ctx, in, out}

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
	in  chan string
	out chan *Player
}

type PWorker struct {
	client *APIClient
	in     chan *PWorkerPayload
}

func (w PWorker) process() {
	for p := range w.in {
	processLoop:
		for item := range p.in {
			// TODO: search in cache
			select {
			case <-p.ctx.Done():
				break processLoop
			case p.out <- w.getPlayer(p.ctx, item):
				break
			}
		}
		close(p.out)
	}

}

func (w PWorker) getPlayer(ctx context.Context, pID string) *Player {
	player, err := w.client.PlayerInfo(ctx, pID)
	if err != nil {
		log.Println("Error getting player data", err)
		return nil
	}
	return player
}

type PWorkerPool struct {
	nworkers int
	workers  []PWorker

	in chan *PWorkerPayload
}

func setupPWorkerPool(nworkers int, c *APIClient) *PWorkerPool {
	in := make(chan *PWorkerPayload, nworkers)
	workers := make([]PWorker, nworkers)
	for i := 0; i < nworkers; i++ {
		workers[i] = PWorker{c, in}
		go workers[i].process()
	}

	return &PWorkerPool{nworkers, workers, in}
}

func (pwp *PWorkerPool) send(ctx context.Context, players []string) <-chan *Player {
	in := make(chan string, pwp.nworkers)
	out := make(chan *Player, pwp.nworkers)
	// TODO: Run task on multiple workers? Or set one tasks set for each worker?
	pwp.in <- &PWorkerPayload{ctx, in, out}

	go func() {
		for _, p := range players {
			in <- p
		}
		close(in)
	}()

	return out
}

// func (pwp *PWorkerPool) stop() {
// 	close(pwp.in)
// }

// TODO: add cache
// TODO: add workers
// TODO: add routing
// TODO: add logging

type Server struct {
	*http.Server
	API *APIClient

	twp *TWorkerPool
	pwp *PWorkerPool

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
	api := NewAPIClient()
	return &Server{Server: s, API: api, done: make(chan bool), twp: setupTWorkerPool(5, api), pwp: setupPWorkerPool(5, api)}

}

func getPlayerComrades(ctx context.Context, twp *TWorkerPool, pID string, tournaments []Tournament) map[string]int {
	out := twp.send(ctx, tournaments)

	comrades := make(map[string]int)
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

func getPlayerComradesInfo(ctx context.Context, pwp *PWorkerPool, comrades map[string]int) []Player {
	pp := make([]string, 0)
	for id := range comrades {
		pp = append(pp, id)
	}

	out := pwp.send(ctx, pp)

	var players []Player
	for pl := range out {
		if pl != nil {
			pl.Games = comrades[pl.IDplayer]
			players = append(players, *pl)
		}
	}
	return players
}

func (s *Server) Handler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	pID := vars["id"]
	ctx := r.Context()
	tournaments, err := s.API.ListPlayerTournaments(ctx, pID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	comrades := getPlayerComrades(ctx, s.twp, pID, tournaments)
	players := getPlayerComradesInfo(ctx, s.pwp, comrades)

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
