package converter

type Converter interface {
	// get converter name
	Name() string
	// convert raw sub uri to the format of different client
	Convert([]string) (string, error)
}
