package main

import (
	"log"
	"net/http"
	"time"
	_ "time/tzdata"

	"sharetab/service/internal/app"
	"sharetab/service/internal/db"
	"sharetab/service/internal/httpapi"
)

func main() {
	cfg, err := app.LoadConfig()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	database, err := db.Open(cfg.DatabaseDSN())
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}

	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.NewHandler(cfg, database),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("ShareTab service listening on %s", server.Addr)
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("serve: %v", err)
	}
}
