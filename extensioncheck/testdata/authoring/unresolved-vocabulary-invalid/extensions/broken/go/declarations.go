package broken

type Declaration struct{}

func Broken(name string) Declaration {
	return Declaration{}
}
