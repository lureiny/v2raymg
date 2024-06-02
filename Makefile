TOOLS = "tools"

v2ray: ${TOOLS}
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags v2ray -o bin/v2raymg main.go

xray: ${TOOLS}
	@CGO_ENABLED=0 go build -gcflags "-N -l" -tags xray -o bin/v2raymg main.go 

${TOOLS}:
	go build -gcflags "-N -l" -o bin/tools cli/*.go
