build:
	go build -o backup-daemon .

windows:
	GOOS=windows GOARCH=amd64 go build -ldflags "-H=windowsgui -s -w" -o BackupAssistant.exe .
