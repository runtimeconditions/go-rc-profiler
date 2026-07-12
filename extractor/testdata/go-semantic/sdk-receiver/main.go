package main

import (
	"context"

	"github.com/example/runtimeconditions/semantic-sdk-receiver/service/events"
)

func writeAuditLog(ctx context.Context, client *events.Client) error {
	return client.Publish(ctx, events.Event{})
}
