build:
	go build -o backup-daemon .

windows:
	go build -ldflags "-H=windowsgui -s -w" -o BackupAssistant.exe .
