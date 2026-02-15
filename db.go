package main

import (
	"database/sql"
	"log"
	"time"

	_ "modernc.org/sqlite"
)

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

func recordHistory(success bool, output string) {
	if db == nil {
		return
	}
	_, err := db.Exec("INSERT INTO history (timestamp, success, output) VALUES (?, ?, ?)", time.Now(), success, output)
	if err != nil {
		log.Printf("Erreur écriture DB historique: %v", err)
	}
}

func hasSuccessfulBackupToday() (bool, error) {
	now := time.Now()
	startOfDay := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM history WHERE success = 1 AND timestamp >= ?", startOfDay).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}
