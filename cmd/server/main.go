package main

import (
	"log"

	"github.com/devravik/go-license-api/internal/server"
	"github.com/gofiber/fiber/v3"
)

func main() {
	serverInst, cfg := server.New()

	log.Fatal(serverInst.Listen(":"+cfg.AppPort, fiber.ListenConfig{
		EnablePrefork: cfg.AppEnv == "production",
	}))
}
