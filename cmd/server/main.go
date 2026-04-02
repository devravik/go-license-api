package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/devravik/go-license-api/internal/server"
	"github.com/gofiber/fiber/v3"
)

func main() {
	serverInst, cfg := server.New()

	// Start server in a separate goroutine
	go func() {
		if err := serverInst.Listen(":"+cfg.AppPort, fiber.ListenConfig{
			EnablePrefork: cfg.AppEnv == "production",
		}); err != nil {
			// Listen returns after Shutdown — suppress error log here
		}
	}()

	// Trap SIGTERM/SIGINT
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGINT)
	<-quit

	// Graceful shutdown window
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	// Ordered shutdown
	println("shutdown: signal received")
	serverInst.Shutdown(shutdownCtx)
}
