package events

import "context"

type Config struct{}
type Event struct{}
type Client struct{}

func NewClient(cfg Config) *Client {
	return &Client{}
}

func (c *Client) Publish(ctx context.Context, event Event) error {
	return nil
}
