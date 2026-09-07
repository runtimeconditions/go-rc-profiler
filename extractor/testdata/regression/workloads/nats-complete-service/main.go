package main

import (
	"context"
	"log"
	"time"

	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

func main() {
	ctx := context.Background()
	connection, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()

	if err := connection.Publish("orders.created", []byte(`{"order":"123"}`)); err != nil {
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

	js, err := jetstream.New(connection)
	if err != nil {
		log.Fatal(err)
	}
	streamConfig := jetstream.StreamConfig{Name: "ORDERS", Subjects: []string{"orders.>"}}
	stream, err := js.CreateStream(ctx, streamConfig)
	if err != nil {
		log.Fatal(err)
	}
	if _, err := stream.Info(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := js.UpdateStream(ctx, streamConfig); err != nil {
		log.Fatal(err)
	}
	if err := js.DeleteStream(ctx, "ARCHIVE"); err != nil {
		log.Fatal(err)
	}
	if _, err := js.Publish(ctx, "orders.created", []byte(`{"order":"123"}`)); err != nil {
		log.Fatal(err)
	}

	consumer, err := js.CreateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{Name: "order-worker"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := consumer.Info(ctx); err != nil {
		log.Fatal(err)
	}
	if _, err := consumer.FetchNoWait(1); err != nil {
		log.Fatal(err)
	}
	if _, err := js.UpdateConsumer(ctx, "ORDERS", jetstream.ConsumerConfig{Name: "order-worker"}); err != nil {
		log.Fatal(err)
	}
	streamConsumer, err := stream.CreateConsumer(ctx, jetstream.ConsumerConfig{Name: "stream-worker"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := streamConsumer.Info(ctx); err != nil {
		log.Fatal(err)
	}
	if err := js.DeleteConsumer(ctx, "ORDERS", "old-worker"); err != nil {
		log.Fatal(err)
	}

	keyValues, err := js.CreateKeyValue(ctx, jetstream.KeyValueConfig{Bucket: "profiles"})
	if err != nil {
		log.Fatal(err)
	}
	revision, err := keyValues.PutString(ctx, "current", "production")
	if err != nil {
		log.Fatal(err)
	}
	if _, err := keyValues.GetRevision(ctx, "current", revision); err != nil {
		log.Fatal(err)
	}
	if _, err := keyValues.Status(ctx); err != nil {
		log.Fatal(err)
	}
	watcher, err := keyValues.WatchAll(ctx)
	if err != nil {
		log.Fatal(err)
	}
	watcher.Stop()
	if err := js.DeleteKeyValue(ctx, "retired-profiles"); err != nil {
		log.Fatal(err)
	}

	objects, err := js.CreateObjectStore(ctx, jetstream.ObjectStoreConfig{Bucket: "configuration"})
	if err != nil {
		log.Fatal(err)
	}
	if _, err := objects.PutString(ctx, "service.yaml", "replicas: 3"); err != nil {
		log.Fatal(err)
	}
	if _, err := objects.GetString(ctx, "service.yaml"); err != nil {
		log.Fatal(err)
	}
	if _, err := objects.Status(ctx); err != nil {
		log.Fatal(err)
	}
	objectWatcher, err := objects.Watch(ctx)
	if err != nil {
		log.Fatal(err)
	}
	objectWatcher.Stop()
	if err := js.DeleteObjectStore(ctx, "retired-configuration"); err != nil {
		log.Fatal(err)
	}

	otherConnection, err := nats.Connect("nats://audit.internal:4222")
	if err != nil {
		log.Fatal(err)
	}
	defer otherConnection.Close()
	if err := otherConnection.Publish("audit.created", []byte("123")); err != nil {
		log.Fatal(err)
	}
}
