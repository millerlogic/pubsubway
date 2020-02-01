package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
)

func run() error {
	addr := ":8080"
	flag.StringVar(&addr, "addr", addr, "HTTP server listen address")
	flag.Parse()

	ps := NewPubSub()
	defer ps.Close()

	http.Handle("/subscribe", http.HandlerFunc(ps.Subscribe))
	http.Handle("/publish", http.HandlerFunc(ps.Publish))

	return http.ListenAndServe(addr, nil)
}

func main() {
	err := run()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %+v\n", err)
		log.Printf("ERROR %v", err)
		os.Exit(1)
	}
}
