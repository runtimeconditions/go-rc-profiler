package broken

type Declaration struct{}

func Present(name string) Declaration {
	return Declaration{}
}
