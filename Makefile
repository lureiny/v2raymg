v2ray:
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags v2ray -o bin/v2raymg main.go

xray:
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags xray -o bin/v2raymg main.go 

tools:
    # @CGO_ENABLED=0 go build -gcflags "-N -l" -o bin/tools cli/cli.go cli/config.go cli/info.go cli/suggest.go
	go build -gcflags "-N -l" -o bin/tools cli/*.go
