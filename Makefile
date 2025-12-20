build:
	go build -ldflags "-H=windowsgui" -o backup-daemon.exe main.go
