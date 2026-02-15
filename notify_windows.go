//go:build windows

package main

import (
	"log"
	"os"
	"path/filepath"

	"gopkg.in/toast.v1"
)

func sendNotification(success bool, title, message string) {
	// Utilisation de l'icône par défaut de l'app si possible, sinon rien
	iconPath := ""
	exePath, err := os.Executable()
	if err == nil {
		potentialIcon := filepath.Join(filepath.Dir(exePath), IconFilename)
		if _, err := os.Stat(potentialIcon); err == nil {
			iconPath = potentialIcon
		}
	}

	notification := &toast.Notification{
		AppID:   "Assistant Backup Doctorat",
		Title:   title,
		Message: message,
		Icon:    iconPath,
		Actions: []toast.Action{
			{Type: "protocol", Label: "Ouvrir Logs", Arguments: "cmd /c start \"\" \"" + paths.logFile + "\""},
		},
	}

	err = notification.Push()
	if err != nil {
		log.Printf("Erreur envoi notification: %v", err)
	}
}
