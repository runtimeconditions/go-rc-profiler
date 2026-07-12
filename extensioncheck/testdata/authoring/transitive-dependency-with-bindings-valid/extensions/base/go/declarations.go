package base

const Redis = "redis"

type Declaration struct{}

func Cache(name string) Declaration {
	return Declaration{}
}
