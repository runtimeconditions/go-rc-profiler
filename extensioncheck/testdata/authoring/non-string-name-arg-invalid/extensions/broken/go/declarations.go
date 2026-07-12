package broken

type Declaration struct{}

func Cache(name int) Declaration {
	return Declaration{}
}
