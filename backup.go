package main

import (
	"log"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/getlantern/systray"
)

type ResticManager struct {
	Config Config
}

func NewResticManager(cfg Config) *ResticManager {
	return &ResticManager{Config: cfg}
}

func (m *ResticManager) RunBackup(statusItem *systray.MenuItem) {
	backupMutex.Lock()
	if isRunning {
		backupMutex.Unlock()
		log.Println("Backup ignorée car une autre est déjà en cours.")
		sendNotification(false, "Backup ignorée", "Une sauvegarde est déjà en cours d'exécution.")
		return
	}
	isRunning = true
	statusItem.SetTitle("Backup en cours...")
	backupMutex.Unlock()

	success, output := m.performBackup()

	backupMutex.Lock()
	isRunning = false
	statusMsg := "Echec dernière backup"
	if success {
		statusMsg = "Prêt (Succès: " + time.Now().Format("15:04") + ")"
	}
	statusItem.SetTitle(statusMsg)
	backupMutex.Unlock()

	recordHistory(success, output)
}

func (m *ResticManager) performBackup() (bool, string) {
	log.Println("Exécution de Restic...")

	args := []string{"backup", m.Config.SourcePath}
	if len(m.Config.ExtraArgs) > 0 {
		args = append(args, m.Config.ExtraArgs...)
	}

	cmd := exec.Command("restic", args...)

	// Environment variables
	env := os.Environ()
	env = append(env, "RESTIC_PASSWORD="+m.Config.ResticPassword)
	env = append(env, "RESTIC_REPOSITORY="+m.Config.Repository)

	// Handle SSH options to avoid interaction issues
	if strings.HasPrefix(m.Config.Repository, "sftp:") {
		sshCmd := "ssh"
		if m.Config.SSHArgs != "" {
			sshCmd = "ssh " + m.Config.SSHArgs
		} else {
			// Default to accept-new to avoid blocking on unknown host keys
			// while still providing some security.
			sshCmd = "ssh -o StrictHostKeyChecking=accept-new"
		}
		log.Printf("Using SSH command: %s", sshCmd)
		env = append(env, "RESTIC_SSH_COMMAND="+sshCmd)
	}

	cmd.Env = env

	// Capture output
	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	success := err == nil

	if success {
		log.Println("--- Sauvegarde Restic TERMINÉE AVEC SUCCÈS ---")
		sendNotification(true, "Sauvegarde Réussie", "Vos données sont en sécurité.")
	} else {
		log.Printf("--- ECHEC SAUVEGARDE RESTIC --- Erreur: %v\nOutput:\n%s", err, outputStr)
		sendNotification(false, "ECHEC SAUVEGARDE", "Une erreur est survenue. Cliquez pour voir les logs.")
	}

	return success, outputStr
}

func checkAndRunCatchUp(statusItem *systray.MenuItem) {
	success, err := hasSuccessfulBackupToday()
	if err != nil {
		log.Printf("Erreur vérification DB pour rattrapage: %v", err)
		return
	}

	if !success {
		log.Println("Aucune sauvegarde réussie détectée aujourd'hui. Lancement du rattrapage au démarrage...")
		if resticMgr != nil {
			resticMgr.RunBackup(statusItem)
		} else {
			// Fallback if not yet initialized for some reason
			m := NewResticManager(appConfig)
			m.RunBackup(statusItem)
		}
	} else {
		log.Println("Une sauvegarde a déjà été effectuée aujourd'hui. Pas de rattrapage nécessaire.")
	}
}
