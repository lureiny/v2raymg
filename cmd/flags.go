package cmd

var (
	// rootCmd flags
	host string
	port int
	// Required flags
	email      string
	inBoundTag string

	// Not necessary flags
	uuid       string
	protocol   string
	alterID    int
	level      int
	configFile string

	// query flags
	unit string
)

type RuntimeConfig struct {
	Host       string `json:"host"`
	Port       int    `json:"port`
	ConfigFile string `json:"config"`
}
