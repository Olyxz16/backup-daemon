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
	"gopkg.in/yaml.v3"
	_ "modernc.org/sqlite"
)

// --- STRUCTURE DE CONFIGURATION YAML ---
type Config struct {
	ResticPassword string `yaml:"restic_password"`
	SourcePath     string `yaml:"source_path"`
	Repository     string `yaml:"repository"` // Renommé pour plus de clarté (ex: sftp:...)
	CronSchedule   string `yaml:"cron_schedule"`
	SSHKeyPath     string `yaml:"ssh_key_path"` // Nouveau champ optionnel
}
// --- VARIABLES GLOBALES ---
var (
	db          *sql.DB
	backupMutex sync.Mutex
	isRunning   bool
	appConfig   Config
	paths       struct {
		appDir  string
		logFile string
		dbFile  string
		configFile string
	}
)

const IconFilename = "icon.ico"

// --- INITIALISATION (Avant l'interface graphique) ---
func init() {
	// 1. Définir les chemins standards Windows (%AppData%/Roaming)
	configDir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal("Impossible de trouver le dossier de configuration utilisateur:", err)
	}
	paths.appDir = filepath.Join(configDir, "ResticBackupAssistant")
	paths.logFile = filepath.Join(paths.appDir, "backup.log")
	paths.dbFile = filepath.Join(paths.appDir, "history.db")
	paths.configFile = filepath.Join(paths.appDir, "config.yaml")

	// Créer le dossier si inexistant
	if _, err := os.Stat(paths.appDir); os.IsNotExist(err) {
		os.MkdirAll(paths.appDir, 0755)
	}

	// 2. Initialisation Logs vers fichier standard
	f, err := os.OpenFile(paths.logFile, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0666)
	if err != nil {
		// Si on ne peut pas logger dans le fichier, on panic sur la sortie standard
		panic(fmt.Sprintf("Erreur fatale log: %v", err))
	}
	log.SetOutput(f)
	log.Println("========== Démarrage de l'agent ==========")
	log.Printf("Dossier de données: %s\n", paths.appDir)

	// 3. Vérifier la présence de Restic dans le PATH
	if _, err := exec.LookPath("restic"); err != nil {
		errMsg := "ERREUR CRITIQUE: 'restic' n'est pas trouvé dans le PATH système. Veuillez installer Restic."
		log.Println(errMsg)
		sendNotification(false, "Restic introuvable !", "L'exécutable restic est absent du PATH.")
		os.Exit(1) // On quitte si restic n'est pas là
	}
}

func main() {
	// On lance le systray. C'est lui qui gère la boucle principale.
	systray.Run(onReady, onExit)
}

func onReady() {
	// Tenter de charger l'icône (doit être à côté de l'exécutable)
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	iconData, err := ioutil.ReadFile(filepath.Join(exeDir, IconFilename))
	if err == nil {
		systray.SetIcon(iconData)
	} else {
		log.Println("Info: icon.ico non trouvée à côté de l'exe.")
		systray.SetTitle("Backup")
	}

	systray.SetTooltip("Assistant de Backup Doctorat")

	mStatus := systray.AddMenuItem("Initialisation...", "Statut")
	mStatus.Disable()
	systray.AddSeparator()
	mBackupNow := systray.AddMenuItem("Sauvegarder maintenant", "Forcer")
	mOpenLogs := systray.AddMenuItem("Ouvrir les logs", "Voir le fichier log")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quitter", "Arrêter")

	// --- Goroutine de Logique Principale ---
	go func() {
		// 4. Charger la configuration YAML
		if err := loadConfig(); err != nil {
			log.Printf("Erreur config: %v", err)
			mStatus.SetTitle("Erreur Configuration (voir logs)")
			sendNotification(false, "Erreur Config", "Impossible de lire config.yaml. Vérifiez les logs.")
			return // On arrête la logique mais on laisse le tray pour accès aux logs
		}

		// 5. Init DB
		initDB()

		// 6. Setup Cron
		c := cron.New()
		_, err := c.AddFunc(appConfig.CronSchedule, func() {
			log.Println(">>> Déclenchement Cron automatique")
			runBackupSafe(mStatus)
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

		// 7. Catch-up (Rattrapage au démarrage)
		// Si aucune backup réussie aujourd'hui, on lance.
		checkAndRunCatchUp(mStatus)
	}()

	// --- Gestion des évènements Menu ---
	for {
		select {
		case <-mBackupNow.ClickedCh:
			log.Println(">>> Déclenchement manuel utilisateur")
			go runBackupSafe(mStatus)
		case <-mOpenLogs.ClickedCh:
			// Ouvre le fichier de log avec l'éditeur par défaut (notepad)
			exec.Command("cmd", "/c", "start", "", paths.logFile).Start()
		case <-mQuit.ClickedCh:
			systray.Quit()
			return
		}
	}
}

func onExit() {
	if db != nil {
		db.Close()
	}
	log.Println("Arrêt de l'application.")
}

// --- FONCTIONS UTILITAIRES ---

func loadConfig() error {
	// Vérifier si le fichier existe
	if _, err := os.Stat(paths.configFile); os.IsNotExist(err) {
		// Créer un template si inexistant pour aider l'utilisateur
		template := `restic_password: "CHANGE_MOI_VITE"
source_path: "C:\\Users\\Doctorant\\Documents\\These"
cron_schedule: "0 9 * * *" # Format Cron: Minute Heure Jour Mois Semaine (ex: 9h00 tous les jours)
`
		ioutil.WriteFile(paths.configFile, []byte(template), 0644)
		return fmt.Errorf("fichier %s inexistant. Un modèle a été créé. Veuillez le configurer", paths.configFile)
	}

	data, err := ioutil.ReadFile(paths.configFile)
	if err != nil {
		return fmt.Errorf("lecture fichier: %w", err)
	}

	err = yaml.Unmarshal(data, &appConfig)
	if err != nil {
		return fmt.Errorf("parsing YAML: %w", err)
	}

	// Validation basique
	if appConfig.ResticPassword == "" || appConfig.SourcePath == "" {
		return fmt.Errorf("restic_password ou source_path vide dans la config")
	}
	if appConfig.ResticPassword == "CHANGE_MOI_VITE" {
		return fmt.Errorf("le mot de passe par défaut n'a pas été changé dans config.yaml")
	}

	return nil
}

func initDB() {
	var err error
	db, err = sql.Open("sqlite", paths.dbFile)
	if err != nil {
		log.Fatalf("Erreur fatale DB: %v", err)
	}
	query := `CREATE TABLE IF NOT EXISTS history (id INTEGER PRIMARY KEY AUTOINCREMENT, timestamp DATETIME, success BOOLEAN, output TEXT);`
	if _, err := db.Exec(query); err != nil {
		log.Fatalf("Erreur init table DB: %v", err)
	}
}

func runBackupSafe(statusItem *systray.MenuItem) {
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

	success := performBackup()

	backupMutex.Lock()
	isRunning = false
	statusMsg := "Echec dernière backup"
	if success {
		statusMsg = "Prêt (Succès: " + time.Now().Format("15:04") + ")"
	}
	statusItem.SetTitle(statusMsg)
	backupMutex.Unlock()
}

func performBackup() bool {
	log.Println("Exécution de Restic...")

	args := []string{"-r", appConfig.Repository, "backup", appConfig.SourcePath}

	cmd := exec.Command("restic", args...)
	cmd.Env = append(os.Environ(), "RESTIC_PASSWORD="+appConfig.ResticPassword)
	
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

	if db != nil {
		_, dbErr := db.Exec("INSERT INTO history (timestamp, success, output) VALUES (?, ?, ?)", time.Now(), success, outputStr)
		if dbErr != nil {
			log.Printf("Erreur écriture DB historique: %v", dbErr)
		}
	}

    return success
}

// Logique de rattrapage simplifiée : A-t-on sauvegardé AUJOURD'HUI ?
func checkAndRunCatchUp(statusItem *systray.MenuItem) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM history WHERE success = 1 AND timestamp >= ?", startOfDay).Scan(&count)
	if err != nil {
		log.Printf("Erreur vérification DB pour rattrapage: %v", err)
		return
	}

	if count == 0 {
		log.Println("Aucune sauvegarde réussie détectée aujourd'hui. Lancement du rattrapage au démarrage...")
		runBackupSafe(statusItem)
	} else {
		log.Println("Une sauvegarde a déjà été effectuée aujourd'hui. Pas de rattrapage nécessaire.")
	}
}

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

	notification.Push()
}
