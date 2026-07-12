package nested

type Option struct{}

func Cache(name string, options ...Option) Option {
	return Option{}
}

func Env(property, name string, options ...Option) Option {
	return Option{}
}

func Sensitive() Option {
	return Option{}
}
