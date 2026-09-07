package main

import (
	"log"
	"time"

	"github.com/nats-io/nats.go"
)

const eventsSubject = "orders.created"

func main() {
	connection, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()

	if err := connection.Publish(eventsSubject, []byte(`{"order":"123"}`)); err != nil {
		log.Fatal(err)
	}
	if _, err := connection.Subscribe("orders.fulfilled", func(message *nats.Msg) {
		log.Printf("fulfilled: %s", message.Data)
	}); err != nil {
		log.Fatal(err)
	}
	if _, err := connection.Request("inventory.reserve", []byte("123"), 2*time.Second); err != nil {
		log.Fatal(err)
	}
}
