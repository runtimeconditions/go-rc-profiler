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
	store, err := js.CreateKeyValue(context.Background(), jetstream.KeyValueConfig{Bucket: "profiles"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := store.Put(context.Background(), "current", []byte("production")); err != nil {
		log.Fatal(err)
	}
	if _, err := store.Get(context.Background(), "current"); err != nil {
		log.Fatal(err)
	}
	watcher, err := store.Watch(context.Background(), "current")
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Stop()
}
