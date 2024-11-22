package api

import (
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/mr-karan/logchef/internal/config"
	"github.com/mr-karan/logchef/internal/db"
	"github.com/mr-karan/logchef/internal/models"
)

type Server struct {
	echo       *echo.Echo
	cfg        *config.Config
	sqlite     *db.SQLite
	clickhouse *db.Clickhouse
}

func NewServer(cfg *config.Config) (*Server, error) {
	// Initialize JSON logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	e := echo.New()
	// Configure echo with our slog logger
	e.Logger = echo.New().Logger
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogError:    true,
		HandleError: true, // forwards error to the global error handler, so it can decide appropriate status code
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error == nil {
				slog.Info("request",
					"uri", v.URI,
					"status", v.Status,
					"method", c.Request().Method,
				)
			} else {
				slog.Error("request error",
					"uri", v.URI,
					"status", v.Status,
					"method", c.Request().Method,
					"error", v.Error,
				)
			}
			return nil
		},
	}))

	sqlite, err := db.NewSQLite(cfg.SQLite.Path)
	if err != nil {
		return nil, err
	}

	clickhouse := db.NewClickhouse()

	s := &Server{
		echo:       e,
		cfg:        cfg,
		sqlite:     sqlite,
		clickhouse: clickhouse,
	}

	sourceRepo := models.NewSourceRepository(sqlite.DB())
	sourceHandler := NewSourceHandler(sourceRepo, clickhouse)

	// Register routes
	api := e.Group("/api")
	sources := api.Group("/sources")
	sources.GET("", sourceHandler.List)
	sources.POST("", sourceHandler.Create)
	sources.GET("/:id", sourceHandler.Get)
	sources.PUT("/:id", sourceHandler.Update)
	sources.DELETE("/:id", sourceHandler.Delete)
	api.GET("", func(c echo.Context) error {
		return c.JSON(http.StatusOK, NewResponse(map[string]string{
			"message": "Welcome to logchef API. Visit /docs to see the API documentation.",
		}))
	})

	return s, nil
}

func (s *Server) Start() error {
	// Log the server start
	slog.Info("Starting server", "host", s.cfg.Server.Host, "port", s.cfg.Server.Port)
	return s.echo.Start(s.cfg.Server.Host + ":" + strconv.Itoa(s.cfg.Server.Port))
}
