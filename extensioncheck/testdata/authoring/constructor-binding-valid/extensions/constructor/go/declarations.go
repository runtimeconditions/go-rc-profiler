package constructor

type Client struct{}
type Declaration struct{}

func NewClient() *Client {
	return &Client{}
}

func (c *Client) Publish(name string) Declaration {
	return Declaration{}
}
