package broken

type Declaration struct{}
type Engine string

const Redis Engine = "rediss"

func Broken(name string) Declaration {
	return Declaration{}
}
