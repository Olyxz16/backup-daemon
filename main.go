package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"

	"github.com/getlantern/systray"
	"github.com/robfig/cron/v3"
)

// --- VARIABLES GLOBALES ---
var (
	db          *sql.DB
	backupMutex sync.Mutex
	isRunning   bool
	appConfig   Config
	resticMgr   *ResticManager
	paths       struct {
		appDir     string
		logFile    string
		dbFile     string
		configFile string
	}
)

const IconFilename = "icon.ico"

func init() {
	// 1. Définir les chemins standards
	appDir, err := getAppDir()
	if err != nil {
		log.Fatal("Impossible de trouver le dossier de configuration utilisateur:", err)
	}
	paths.appDir = appDir
	paths.logFile = filepath.Join(paths.appDir, "backup.log")
	paths.dbFile = filepath.Join(paths.appDir, "history.db")
	paths.configFile = filepath.Join(paths.appDir, "config.yaml")

	// 2. Initialisation Logs
	f, err := os.OpenFile(paths.logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		panic(fmt.Sprintf("Erreur fatale log: %v", err))
	}
	log.SetOutput(f)
	log.Println("========== Démarrage de l'agent ==========")
	log.Printf("Dossier de données: %s\n", paths.appDir)

	// 3. Vérifier la présence de Restic dans le PATH
	if _, err := exec.LookPath("restic"); err != nil {
		errMsg := "ERREUR CRITIQUE: 'restic' n'est pas trouvé dans le PATH système."
		log.Println(errMsg)
		sendNotification(false, "Restic introuvable !", "L'exécutable restic est absent du PATH.")
		os.Exit(1)
	}
}

func main() {
	systray.Run(onReady, onExit)
}

func onReady() {
	setupTray()
}

func onExit() {
	if db != nil {
		db.Close()
	}
	log.Println("Arrêt de l'application.")
}

func setupTray() {
	// Icon setup
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	iconData, err := ioutil.ReadFile(filepath.Join(exeDir, IconFilename))
	if err == nil {
		systray.SetIcon(iconData)
	} else {
		log.Println("Info: icon.ico non trouvée à côté de l'exe.")
		systray.SetTitle("Backup")
	}

	systray.SetTooltip("Assistant de Backup")

	mStatus := systray.AddMenuItem("Initialisation...", "Statut")
	mStatus.Disable()
	systray.AddSeparator()
	mBackupNow := systray.AddMenuItem("Sauvegarder maintenant", "Forcer")
	mOpenLogs := systray.AddMenuItem("Ouvrir les logs", "Voir le fichier log")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quitter", "Arrêter")

	go func() {
		if err := loadConfig(); err != nil {
			log.Printf("Erreur config: %v", err)
			mStatus.SetTitle("Erreur Configuration (voir logs)")
			sendNotification(false, "Erreur Config", err.Error())
			return
		}

		initDB()
		resticMgr = NewResticManager(appConfig)

		c := cron.New()
		_, err := c.AddFunc(appConfig.CronSchedule, func() {
			log.Println(">>> Déclenchement Cron automatique")
			resticMgr.RunBackup(mStatus)
		})
		if err != nil {
			log.Printf("Erreur format Cron '%s': %v", appConfig.CronSchedule, err)
			mStatus.SetTitle("Erreur Cron (voir logs)")
			sendNotification(false, "Erreur Cron", "Le format de l'heure dans le config.yaml est invalide.")
			return
		}
		c.Start()
		log.Printf("Planificateur démarré avec la règle: [%s]", appConfig.CronSchedule)

		mStatus.SetTitle("Prêt - En attente")

		checkAndRunCatchUp(mStatus)
	}()

	for {
		select {
		case <-mBackupNow.ClickedCh:
			log.Println(">>> Déclenchement manuel utilisateur")
			if resticMgr != nil {
				go resticMgr.RunBackup(mStatus)
			} else {
				log.Println("Erreur: resticMgr non initialisé")
			}
		case <-mOpenLogs.ClickedCh:
			openLogs()
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func openLogs() {
	// Cross-platform open (mostly focused on Windows as per original code)
	var cmd *exec.Cmd
	if _, err := os.Stat("/usr/bin/open"); err == nil {
		cmd = exec.Command("open", paths.logFile)
	} else if _, err := os.Stat("/usr/bin/xdg-open"); err == nil {
		cmd = exec.Command("xdg-open", paths.logFile)
	} else {
		cmd = exec.Command("cmd", "/c", "start", "", paths.logFile)
	}
	cmd.Start()
}
