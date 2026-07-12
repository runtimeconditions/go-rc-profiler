package base

type Declaration struct{}
type Engine string
type CacheOption interface {
	baseCacheOption()
}

type cacheOption struct{}

func (cacheOption) baseCacheOption() {}

const (
	Redis     Engine = "redis"
	Memcached Engine = "memcached"
)

func Cache(name string, options ...CacheOption) Declaration {
	return Declaration{}
}

func KeyValue(engine Engine) CacheOption {
	return cacheOption{}
}
