package main

import (
	"log"
	"net/http"

	"webrts/server/internal/game"
	httpserver "webrts/server/internal/http"
	"webrts/server/internal/ws"
)

func main() {
	manager := game.NewMatchManager()
	hub := ws.NewHub(manager)
	router := httpserver.NewRouter(hub)

	addr := ":8080"
	log.Printf("server listening on %s", addr)

	if err := http.ListenAndServe(addr, router); err != nil {
		log.Fatal(err)
	}
}
