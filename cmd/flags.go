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

	// server's token
	token string
)
