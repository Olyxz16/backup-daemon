package main

import (
	"database/sql"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/getlantern/systray"
	"github.com/robfig/cron/v3"
	"gopkg.in/toast.v1"
	_ "modernc.org/sqlite"
)

// --- CONFIGURATION ---
const (
	ResticExe   = `C:\ProgramData\chocolatey\bin\restic.exe`
	RepoPath    = `B:\MonBackup`
	Password    = "mon_mot_de_passe_secret"
	SourcePath  = `C:\Users\Doctorant\Documents\These`
	BackupHour  = 9
	BackupMin   = 0
	LogFile     = "backup_service.log"
	DbFile      = "backup_history.db"
	IconFile    = "icon.ico" // IMPORTANT : Il faut un fichier .ico à côté de l'exe
)

var (
	db          *sql.DB
	backupMutex sync.Mutex // Pour éviter 2 backups en même temps
	isRunning   bool
)

func main() {
	// 1. Init Logs
	f, err := os.OpenFile(LogFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		log.Fatalf("Erreur log: %v", err)
	}
	defer f.Close()
	log.SetOutput(f)

	// 2. On lance le systray (Ceci bloque le main thread, c'est normal)
	systray.Run(onReady, onExit)
}

func onReady() {
	log.Println("--- Démarrage de l'agent Backup (Tray) ---")
	
	// Charger l'icône
	iconData, err := ioutil.ReadFile(IconFile)
	if err == nil {
		systray.SetIcon(iconData)
	} else {
		log.Println("Attention: icon.ico non trouvé, l'application sera invisible dans le tray sans icône par défaut du système.")
		systray.SetTitle("Backup") // Fallback
	}

	systray.SetTooltip("Assistant de Backup Doctorat")

	// --- MENU ---
	mStatus := systray.AddMenuItem("Prêt", "Statut actuel")
	mStatus.Disable() // Juste informatif
	systray.AddSeparator()
	mBackupNow := systray.AddMenuItem("Sauvegarder maintenant", "Forcer une sauvegarde immédiate")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quitter", "Arrêter le programme")

	// --- INITIALISATION LOGIQUE (Dans une Goroutine séparée) ---
	go func() {
		initDB()
		defer db.Close()

		// Cron
		c := cron.New()
		cronExp := fmt.Sprintf("%d %d * * *", BackupMin, BackupHour)
		c.AddFunc(cronExp, func() {
			log.Println("Déclenchement automatique Cron")
			runBackupSafe(mStatus)
		})
		c.Start()

		// Catch-up (Vérification au démarrage)
		checkAndRunMissedBackup(mStatus)
	}()

	// --- GESTION DES CLICS MENU ---
	go func() {
		for {
			select {
			case <-mBackupNow.ClickedCh:
				log.Println("Déclenchement manuel par l'utilisateur")
				go runBackupSafe(mStatus) // On lance dans une routine pour ne pas geler le menu
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

func onExit() {
	log.Println("Arrêt de l'application.")
}

// Wrapper thread-safe pour la backup
func runBackupSafe(statusItem *systray.MenuItem) {
	// On verrouille pour s'assurer qu'une seule backup tourne à la fois
	backupMutex.Lock()
	if isRunning {
		backupMutex.Unlock()
		log.Println("Backup demandée mais une est déjà en cours. Ignoré.")
		return
	}
	isRunning = true
	statusItem.SetTitle("Backup en cours...")
	backupMutex.Unlock()

	// Exécution
	performBackup()

	// Fin
	backupMutex.Lock()
	isRunning = false
	statusItem.SetTitle("Prêt (Dernière: " + time.Now().Format("15:04") + ")")
	backupMutex.Unlock()
}

// ... (Le reste des fonctions initDB, checkAndRunMissedBackup sont identiques à avant, sauf l'appel à performBackup) ...

func initDB() {
	var err error
	db, err = sql.Open("sqlite", DbFile)
	if err != nil {
		log.Fatal(err)
	}
	query := `CREATE TABLE IF NOT EXISTS history (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME, success BOOLEAN, output TEXT);`
	db.Exec(query)
}

func checkAndRunMissedBackup(statusItem *systray.MenuItem) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	scheduledTime := time.Date(now.Year(), now.Month(), now.Day(), BackupHour, BackupMin, 0, 0, now.Location())

	if now.Before(scheduledTime) {
		return
	}

	var count int
	db.QueryRow("SELECT COUNT(*) FROM history WHERE success = 1 AND timestamp >= ?", startOfDay).Scan(&count)

	if count == 0 {
		log.Println("Rattrapage nécessaire...")
		runBackupSafe(statusItem)
	}
}

func performBackup() {
	log.Println("Exécution Restic...")
	cmd := exec.Command(ResticExe, "-r", RepoPath, "backup", SourcePath)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+Password)
	
	// Optionnel: Pour éviter la fenêtre console popup lors de l'appel exec (si compilé sans -H=windowsgui c'est utile, mais avec c'est mieux)
	// cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} 

	output, err := cmd.CombinedOutput()
	outputStr := string(output)
	success := err == nil

	if success {
		log.Println("Succès.")
	} else {
		log.Printf("Echec: %v", err)
	}

	db.Exec("INSERT INTO history (timestamp, success, output) VALUES (?, ?, ?)", time.Now(), success, outputStr)
	sendNotification(success)
}

func sendNotification(success bool) {
	title := "Sauvegarde Terminée"
	message := "Tout est OK."
	if !success {
		title = "ERREUR SAUVEGARDE"
		message = "Cliquez pour voir les logs."
	}
	
	// On récupère le chemin absolu du log pour l'ouvrir
	absLog, _ := filepath.Abs(LogFile)

	notification := &toast.Notification{
		AppID:   "Backup Assistant",
		Title:   title,
		Message: message,
		Actions: []toast.Action{
			{Type: "protocol", Label: "Voir Logs", Arguments: "notepad " + absLog},
		},
	}

	notification.Push()
}
