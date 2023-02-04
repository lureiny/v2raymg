package cmd

var (
	// rootCmd flags
	host string
	port int
	// Required flags
	email      string
	inBoundTag string
	tag        string
	nodeName   string

	// Not necessary flags
	uuid       string
	alterID    int
	level      int
	configFile string

	// query flags
	unit    string
	pattern string
)
