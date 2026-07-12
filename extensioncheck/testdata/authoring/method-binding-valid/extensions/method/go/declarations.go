package method

type Builder struct{}
type Declaration struct{}

func (b Builder) Cache(name string) Declaration {
	return Declaration{}
}
