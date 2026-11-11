package main

import (
	"fmt"
	"os"
	"os/signal"

	"github.com/gorilla/websocket"
)

// tail prints live stream messages: wraith tail [ws-url]
func tail(args []string) int {
	url := "ws://127.0.0.1:8088/api/v1/ws"
	if len(args) > 0 {
		url = args[0]
	}
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		return 1
	}
	defer conn.Close()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt)
	go func() { <-sig; conn.Close() }()
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return 0
		}
		fmt.Println(string(msg))
	}
}
