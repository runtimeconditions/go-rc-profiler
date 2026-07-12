package env

type Option struct{}

func Env(property, name string) Option {
	return Option{}
}
