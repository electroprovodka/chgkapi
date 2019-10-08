package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"

	"github.com/gorilla/mux"
)

type APIClient struct {
	http.Client
	BaseURL string
}

func NewAPIClient() *APIClient {
	// TODO: add options
	return &APIClient{http.Client{}, "https://rating.chgk.info/api"}
}

func (api APIClient) getURL(path string, items ...interface{}) string {
	return api.BaseURL + fmt.Sprintf(path, items...)
}

func (api APIClient) getPlayerTournaments(pID string) ([]byte, error) {
	url := api.getURL("/players/%s/tournaments.json", pID)
	resp, err := api.Get(url)
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

func (api APIClient) getTournamentPlayers(tID, teamID string) ([]byte, error) {
	url := api.getURL("/tournaments/%s/recaps/%s.json", tID, teamID)
	resp, err := api.Get(url)
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

func (api APIClient) getPlayerInfo(pID string) ([]byte, error) {
	url := api.getURL("/players/%s.json", pID)
	resp, err := api.Get(url)
	if err != nil {
		log.Println("Error retrieving player data", err)
		return nil, err
	}
	pb, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading player data", err)
		return nil, err
	}
	return pb, nil
}

type TWorker struct {
	client *APIClient
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
	resp, err := w.client.getTournamentPlayers(t.IDTournament, t.IDTeam)
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
	resp, err := w.client.getPlayerInfo(pID)
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

func Handler(w http.ResponseWriter, r *http.Request) {
	c := NewAPIClient()

	vars := mux.Vars(r)
	resp, err := c.getPlayerTournaments(vars["id"])
	if err != nil {
		log.Println("Error getting player tournaments", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var seasons map[string]Season
	err = json.Unmarshal(resp, &seasons)
	if err != nil {
		log.Println("Error parsing tournaments response", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var tournaments []Tournament
	for _, s := range seasons {
		tournaments = append(tournaments, s.Tournaments...)
	}

	comrades := make(map[string]int)
	in := make(chan Tournament, 2)
	out := make(chan []string, 2)
	tworker := TWorker{client: c, in: in, out: out}
	go tworker.process()
	for _, t := range tournaments {
		tworker.send(t)
		for _, p := range tworker.receive() {
			comrades[p]++
		}
	}
	close(in)

	// Remove player himself
	delete(comrades, vars["id"])

	var players []Player
	pin := make(chan string, 1)
	pout := make(chan *Player, 1)
	pworker := PWorker{client: c, in: pin, out: pout}
	go pworker.process()

	for id, count := range comrades {
		pworker.send(id)
		pl := pworker.receive()
		if pl != nil {
			pl.Games = count
			players = append(players, *pl)
		}
	}
	close(pin)

	OrderBy(gamesSort, nameSort, surnameSort, patronymicSort).Sort(players)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(players)
}

func main() {
	r := mux.NewRouter()
	r.HandleFunc("/comrades/{id:[0-9]+}", Handler)

	err := http.ListenAndServe(":3000", r)
	if err != nil {
		log.Fatal(err)
	}
}
