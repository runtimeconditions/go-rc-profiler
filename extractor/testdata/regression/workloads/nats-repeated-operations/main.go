// This workload is not one of the reviewed NATS fixtures. It exists so the
// regression suite exercises operation de-duplication within a single
// source-proven dependency identity, which the reviewed fixtures never trigger.
package main

import (
	"log"

	"github.com/nats-io/nats.go"
)

const eventsSubject = "orders.created"

func main() {
	connection, err := nats.Connect(nats.DefaultURL)
	if err != nil {
		log.Fatal(err)
	}
	defer connection.Close()

	// The same subject is published from three syntactically different
	// expressions that all resolve to one compile-time value.
	if err := connection.Publish(eventsSubject, []byte(`{"order":"1"}`)); err != nil {
		log.Fatal(err)
	}
	if err := connection.Publish("orders.created", []byte(`{"order":"2"}`)); err != nil {
		log.Fatal(err)
	}
	subject := eventsSubject
	if err := connection.Publish(subject, []byte(`{"order":"3"}`)); err != nil {
		log.Fatal(err)
	}

	// A distinct subject on the same connection must remain a separate
	// operation rather than collapse into the repeated one.
	if err := connection.Publish("orders.cancelled", []byte(`{"order":"4"}`)); err != nil {
		log.Fatal(err)
	}
}
