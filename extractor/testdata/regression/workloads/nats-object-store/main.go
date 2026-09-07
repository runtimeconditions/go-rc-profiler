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
	store, err := js.CreateObjectStore(context.Background(), jetstream.ObjectStoreConfig{Bucket: "configuration"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := store.PutString(context.Background(), "service.yaml", "replicas: 3"); err != nil {
		log.Fatal(err)
	}
	if _, err := store.GetString(context.Background(), "service.yaml"); err != nil {
		log.Fatal(err)
	}
	watcher, err := store.Watch(context.Background())
	if err != nil {
		log.Fatal(err)
	}
	defer watcher.Stop()
}
