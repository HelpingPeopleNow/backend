package main

import (
	"encoding/json"
	"math/rand"
	"net/http"
)

var phrases = []string{
	"keep going",
	"you've got this",
	"make it happen",
	"stay curious",
	"build something awesome",
	"one step at a time",
	"dream big",
	"code on",
	"never stop learning",
	"be the change",
}

func helloHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	phrase := phrases[rand.Intn(len(phrases))]
	json.NewEncoder(w).Encode(map[string]string{"message": "hello, " + phrase})
}
