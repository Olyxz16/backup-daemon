//go:build linux

package main

import (
	"log"
	"os/exec"
)

func sendNotification(success bool, title, message string) {
	// Simple implementation using notify-send (common on Linux)
	// If notify-send is missing, it will just fail silently in the background
	cmd := exec.Command("notify-send", title, message)
	err := cmd.Run()
	if err != nil {
		log.Printf("Erreur envoi notification (notify-send): %v", err)
	}
}
