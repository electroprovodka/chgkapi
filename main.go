package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"sort"

	"github.com/gorilla/mux"
)

// TODO: add cache
// TODO: add workers
// TODO: add routing
// TODO: add logging

type Player struct {
	IDplayer   string `json:"idplayer"`
	Name       string `json:"name"`
	Surname    string `json:"surname"`
	Patronymic string `json:"patronymic"`
	Games      int    `json:"games"`
}

type lessFunc func(p1, p2 *Player) bool

func gamesSort(p1, p2 *Player) bool {
	return p1.Games < p2.Games
}

func nameSort(p1, p2 *Player) bool {
	return p1.Name < p2.Name
}

func surnameSort(p1, p2 *Player) bool {
	return p1.Surname < p2.Surname
}

func patronymicSort(p1, p2 *Player) bool {
	return p1.Patronymic < p2.Patronymic
}

type playersSorter struct {
	players []Player
	less    []lessFunc
}

func (ps *playersSorter) Sort(players []Player) {
	ps.players = players
	sort.Sort(sort.Reverse(ps))
}

func OrderBy(less ...lessFunc) *playersSorter {
	return &playersSorter{
		less: less,
	}
}

func (ps playersSorter) Len() int      { return len(ps.players) }
func (ps playersSorter) Swap(i, j int) { ps.players[i], ps.players[j] = ps.players[j], ps.players[i] }
func (ps playersSorter) Less(i, j int) bool {
	p, q := &ps.players[i], &ps.players[j]
	var k int
	for k = 0; k < len(ps.less)-1; k++ {
		less := ps.less[k]
		switch {
		case less(p, q):
			return true
		case less(q, p):
			return false
		}
	}
	return ps.less[k](p, q)
}

type Tournament struct {
	IDTournament, IDTeam string
}

type Season struct {
	Tournaments []Tournament
}

func getPlayerInfo(c http.Client, in <-chan string, out chan<- *Player) {
	for id := range in {
		resp, err := c.Get(fmt.Sprintf("https://rating.chgk.info/api/players/%s.json", id))
		if err != nil {
			log.Println("Error retrieving player data", err)
			out <- nil
			continue
		}
		tb, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error reading player data", err)
			out <- nil
			continue
		}

		var pl []Player
		err = json.Unmarshal(tb, &pl)
		if err != nil {
			log.Println("Error parsing player data", err)
			out <- nil
			continue
		}
		out <- &pl[0]
	}
	close(out)
}

func getTournamentPlayers(c http.Client, in <-chan Tournament, out chan<- []string) {
	for t := range in {
		resp, err := c.Get(fmt.Sprintf("https://rating.chgk.info/api/tournaments/%s/recaps/%s.json", t.IDTournament, t.IDTeam))
		if err != nil {
			log.Println("Error retrieving players for tournament", err)
			out <- nil
			continue
		}
		tb, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error reading players for tournament", err)
			out <- nil
			continue
		}

		var td []map[string]string
		err = json.Unmarshal(tb, &td)
		if err != nil {
			log.Println("Error parsing players for tournament", err)
			out <- nil
			continue
		}
		players := make([]string, len(td))
		for i, pl := range td {
			players[i] = pl["idplayer"]
		}
		out <- players
	}
	close(out)
}

func Handler(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	c := http.Client{}
	resp, err := c.Get(fmt.Sprintf("https://rating.chgk.info/api/players/%s/tournaments.json", vars["id"]))
	if err != nil {
		log.Println("Error getting player tournaments", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var sd map[string]Season
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		log.Println("Error reading tournaments response", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	err = json.Unmarshal(data, &sd)
	if err != nil {
		log.Println("Error parsing tournaments response", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	var ts []Tournament
	for _, s := range sd {
		ts = append(ts, s.Tournaments...)
	}

	comrades := make(map[string]int)
	in := make(chan Tournament, 2)
	out := make(chan []string, 2)
	go getTournamentPlayers(c, in, out)
	for _, t := range ts {
		in <- t
		ps := <-out
		for _, p := range ps {
			comrades[p]++
		}
	}
	close(in)

	// Remove player himself
	delete(comrades, vars["id"])

	var players []Player
	pin := make(chan string, 1)
	pout := make(chan *Player, 1)
	go getPlayerInfo(c, pin, pout)

	for id, count := range comrades {
		pin <- id
		pl := <-pout
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
