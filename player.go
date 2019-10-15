package main

import "sort"

// Player is a struct that stores ChGK player info
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

// PlayerSorter is a struct that holds data related to Player objects sorting
type PlayerSorter struct {
	players []Player
	less    []lessFunc
}

// Sort is a function that allows to sort the list of Player objects
func (ps *PlayerSorter) Sort(players []Player) {
	ps.players = players
	sort.Sort(sort.Reverse(ps))
}

// OrderBy is a function that accepts ordering functions and returns sorter for them
func OrderBy(less ...lessFunc) *PlayerSorter {
	return &PlayerSorter{
		less: less,
	}
}

func (ps PlayerSorter) Len() int      { return len(ps.players) }
func (ps PlayerSorter) Swap(i, j int) { ps.players[i], ps.players[j] = ps.players[j], ps.players[i] }
func (ps PlayerSorter) Less(i, j int) bool {
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
