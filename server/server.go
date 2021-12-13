package server

type Server interface {
	Init()
	Start()
	Stop()
	Restart()
}

type ServerConfig struct {
	Host string
	Port int
	Type string
	Name string
}
