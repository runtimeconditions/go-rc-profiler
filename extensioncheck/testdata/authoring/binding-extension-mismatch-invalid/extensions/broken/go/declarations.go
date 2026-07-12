package broken

type Declaration struct{}

func Cache(name string) Declaration {
	return Declaration{}
}
