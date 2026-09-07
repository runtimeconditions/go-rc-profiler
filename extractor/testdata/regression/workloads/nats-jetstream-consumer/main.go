package main

import (
	"context"
	"log"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	connection, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()
	js, err := jetstream.New(connection)
	if err != nil {
		log.Fatal(err)
	}
	consumer, err := js.Consumer(context.Background(), "ORDERS", "order-worker")
	if err != nil {
		log.Fatal(err)
	}
	consumeContext, err := consumer.Consume(func(message jetstream.Msg) {
		log.Printf("order: %s", message.Data())
		_ = message.Ack()
	})
	if err != nil {
		log.Fatal(err)
	}
	defer consumeContext.Stop()
	select {}
}
