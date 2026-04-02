package handlers

import (
	"path/filepath"

	"github.com/gofiber/fiber/v3"
)

func (h *Handler) OpenAPIRawYAML(c fiber.Ctx) error {
	p := filepath.Clean(filepath.Join(".", "docs", "openapi.yaml"))
	return c.SendFile(p)
}

func (h *Handler) OpenAPIRawJSON(c fiber.Ctx) error {
	p := filepath.Clean(filepath.Join(".", "docs", "openapi.json"))
	return c.SendFile(p)
}

func (h *Handler) SwaggerUI(c fiber.Ctx) error {
	html := `<!doctype html>
<html>
  <head>
    <meta charset="utf-8"/>
    <title>Swagger UI</title>
    <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css" />
  </head>
  <body>
    <div id="swagger-ui"></div>
    <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js" crossorigin></script>
    <script>
      window.onload = () => {
        window.ui = SwaggerUIBundle({
          url: '/openapi.json',
          dom_id: '#swagger-ui'
        });
      };
    </script>
  </body>
</html>`
	c.Type("html", "utf-8")
	return c.SendString(html)
}

func (h *Handler) RedocUI(c fiber.Ctx) error {
	html := `<!doctype html>
<html>
  <head>
    <meta charset="utf-8"/>
    <title>Redoc</title>
    <script src="https://cdn.redoc.ly/redoc/latest/bundles/redoc.standalone.js"></script>
  </head>
  <body>
    <redoc spec-url="/openapi.json"></redoc>
  </body>
</html>`
	c.Type("html", "utf-8")
	return c.SendString(html)
}

