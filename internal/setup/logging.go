package setup

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/logger"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	MaxLogSize    = 100 // mb
	MaxLogBackups = 7
	MaxLogAge     = 7 // days
	LogCompress   = true
)

type LoggingConfig struct {
	Enabled bool
	Output  string
	Dir     string
}

func (c *LoggingConfig) Setup(app *fiber.App) {
	if !c.Enabled {
		return
	}

	loggerConfig := logger.Config{
		CustomTags: map[string]logger.LogFunc{
			"requestid": func(output logger.Buffer, c fiber.Ctx, data *logger.Data, extraParam string) (int, error) {
				return output.WriteString(requestid.FromContext(c))
			},
		},
		Format: "${time} [${requestid}] ${status} - ${method} ${path}\n",
		Stream: os.Stdout,
	}

	if strings.EqualFold(c.Output, "file") {
		// Ensure log directory exists
		if err := os.MkdirAll(c.Dir, 0755); err != nil {
			fmt.Printf("Warning: failed to create log directory %s: %v. Falling back to stdout.\n", c.Dir, err)
		} else {
			loggerConfig.Stream = &lumberjack.Logger{
				Filename:   filepath.Join(c.Dir, time.Now().Format("2006-01-02")+".log"),
				MaxSize:    MaxLogSize,
				MaxBackups: MaxLogBackups,
				MaxAge:     MaxLogAge,
				Compress:   LogCompress,
			}
		}
	}

	app.Use(logger.New(loggerConfig))
}

func LoadLoggingConfig() *LoggingConfig {
	return &LoggingConfig{
		Enabled: getEnvBool("LOG_ENABLED", true),
		Output:  getEnv("LOG_OUTPUT", "stdout"),
		Dir:     getEnv("LOG_DIR", "./logs"),
	}
}
