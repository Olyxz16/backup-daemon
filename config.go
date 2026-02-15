package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Repository     string   `yaml:"repository"`
	ResticPassword string   `yaml:"restic_password"`
	SourcePath     string   `yaml:"source_path"`
	CronSchedule   string   `yaml:"cron_schedule"` // Ex: "0 9 * * *"
	SSHArgs        string   `yaml:"ssh_args,omitempty"`
	ExtraArgs      []string `yaml:"extra_args,omitempty"`
}

func loadConfig() error {
	// Vérifier si le fichier existe
	if _, err := os.Stat(paths.configFile); os.IsNotExist(err) {
		// Créer un template si inexistant pour aider l'utilisateur
		template := `repository: "sftp:user@host:/path/to/repo"
restic_password: "CHANGE_MOI_VITE"
source_path: "C:\Users\User\Documents"
cron_schedule: "0 9 * * *" # Format Cron: Minute Heure Jour Mois Semaine (ex: 9h00 tous les jours)
ssh_args: "-o StrictHostKeyChecking=accept-new"
`
		err := ioutil.WriteFile(paths.configFile, []byte(template), 0644)
		if err != nil {
			return fmt.Errorf("création template config: %w", err)
		}
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

	if appConfig.Repository == "" {
		return fmt.Errorf("repository est vide")
	}

	return nil
}

func getAppDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(configDir, "ResticBackupAssistant")
	if _, err := os.Stat(appDir); os.IsNotExist(err) {
		err = os.MkdirAll(appDir, 0755)
		if err != nil {
			return "", err
		}
	}
	return appDir, nil
}
