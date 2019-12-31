package server

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/lukaszbudnik/migrator/common"
	"github.com/lukaszbudnik/migrator/config"
	"github.com/lukaszbudnik/migrator/coordinator"
	"github.com/lukaszbudnik/migrator/types"
)

const (
	defaultPort     string = "8080"
	requestIDHeader string = "X-Request-Id"
)

type migrationsPostRequest struct {
	Response types.MigrationsResponseType `json:"response" binding:"required"`
	Mode     types.MigrationsModeType     `json:"mode" binding:"required"`
}

type tenantsPostRequest struct {
	Name string `json:"name" binding:"required"`
	migrationsPostRequest
}

type migrationsSuccessResponse struct {
	Results           *types.MigrationResults `json:"results"`
	AppliedMigrations []types.Migration       `json:"appliedMigrations,omitempty"`
}

type errorResponse struct {
	ErrorMessage string      `json:"error"`
	Details      interface{} `json:"details,omitempty"`
}

// GetPort gets the port from config or defaultPort
func GetPort(config *config.Config) string {
	if len(strings.TrimSpace(config.Port)) == 0 {
		return defaultPort
	}
	return config.Port
}

func requestIDHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := c.Request.Header.Get(requestIDHeader)
		if requestID == "" {
			requestID = fmt.Sprintf("%d", time.Now().UnixNano())
		}
		ctx := context.WithValue(c.Request.Context(), common.RequestIDKey{}, requestID)
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}

func recovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if err := recover(); err != nil {
				// Check for a broken connection, as it is not really a
				// condition that warrants a panic stack trace.
				var brokenPipe bool
				if ne, ok := err.(*net.OpError); ok {
					if se, ok := ne.Err.(*os.SyscallError); ok {
						if strings.Contains(strings.ToLower(se.Error()), "broken pipe") || strings.Contains(strings.ToLower(se.Error()), "connection reset by peer") {
							brokenPipe = true
						}
					}
				}

				// If the connection is dead, we can't write a status to it.
				if brokenPipe {
					common.LogPanic(c.Request.Context(), "Broken pipe: %v", err)
					c.Error(err.(error)) // nolint: errcheck
					c.Abort()
				} else {
					common.LogPanic(c.Request.Context(), "Panic recovered: %v", err)
					if gin.IsDebugging() {
						debug.PrintStack()
					}
					c.AbortWithStatusJSON(http.StatusInternalServerError, &errorResponse{err.(string), nil})
				}
			}
		}()
		c.Next()
	}
}

func requestLoggerHandler() gin.HandlerFunc {
	return func(c *gin.Context) {
		common.LogInfo(c.Request.Context(), "clientIP=%v method=%v request=%v", c.ClientIP(), c.Request.Method, c.Request.URL.RequestURI())
		c.Next()
	}
}

func makeHandler(config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator, handler func(*gin.Context, *config.Config, func(context.Context, *config.Config) coordinator.Coordinator)) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler(c, config, newCoordinator)
	}
}

func configHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	c.YAML(200, config)
}

func migrationsSourceHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	coordinator := newCoordinator(c.Request.Context(), config)
	defer coordinator.Dispose()
	migrations := coordinator.GetSourceMigrations()
	common.LogInfo(c.Request.Context(), "Returning source migrations: %v", len(migrations))
	c.JSON(http.StatusOK, migrations)
}

func migrationsAppliedHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	coordinator := newCoordinator(c.Request.Context(), config)
	defer coordinator.Dispose()
	dbMigrations := coordinator.GetAppliedMigrations()
	common.LogInfo(c.Request.Context(), "Returning applied migrations: %v", len(dbMigrations))
	c.JSON(http.StatusOK, dbMigrations)
}

func migrationsPostHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	var request migrationsPostRequest

	if err := c.ShouldBindJSON(&request); err != nil {
		common.LogError(c.Request.Context(), "Error reading request: %v", err.Error())
		c.AbortWithStatusJSON(http.StatusInternalServerError, errorResponse{"Invalid request, please see documentation for valid JSON payload", nil})
		return
	}

	if types.ValidateMigrationsMode(request.Mode) == false {
		c.AbortWithStatusJSON(http.StatusBadRequest, errorResponse{fmt.Sprintf("Valid mode parameters are: %v, %v, %v", types.ModeTypeApply, types.ModeTypeSync, types.ModeTypeDryRun), nil})
		return
	}

	// TODO validate results param

	coordinator := newCoordinator(c.Request.Context(), config)
	defer coordinator.Dispose()

	if ok, offendingMigrations := coordinator.VerifySourceMigrationsCheckSums(); !ok {
		common.LogError(c.Request.Context(), "Checksum verification failed for migrations: %v", len(offendingMigrations))
		c.AbortWithStatusJSON(http.StatusFailedDependency, errorResponse{"Checksum verification failed. Please review offending migrations.", offendingMigrations})
		return
	}

	results, appliedMigrations := coordinator.ApplyMigrations()

	common.LogInfo(c.Request.Context(), "Returning applied migrations: %v", len(appliedMigrations))

	c.JSON(http.StatusOK, migrationsSuccessResponse{results, appliedMigrations})
}

func tenantsGetHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	coordinator := newCoordinator(c.Request.Context(), config)
	defer coordinator.Dispose()
	tenants := coordinator.GetTenants()
	common.LogInfo(c.Request.Context(), "Returning tenants: %v", len(tenants))
	c.JSON(http.StatusOK, tenants)
}

func tenantsPostHandler(c *gin.Context, config *config.Config, newCoordinator func(context.Context, *config.Config) coordinator.Coordinator) {
	var tenant tenantsPostRequest
	err := c.ShouldBindJSON(&tenant)
	if err != nil {
		common.LogError(c.Request.Context(), "Bad request: %v", err.Error())
		c.AbortWithStatusJSON(http.StatusBadRequest, errorResponse{"Invalid request, please see documentation for valid JSON payload", nil})
		return
	}

	coordinator := newCoordinator(c.Request.Context(), config)
	defer coordinator.Dispose()
	results, appliedMigrations := coordinator.AddTenantAndApplyMigrations(tenant.Name)

	text := fmt.Sprintf("Tenant %v added, migrations applied: %v", tenant.Name, len(appliedMigrations))

	common.LogInfo(c.Request.Context(), text)
	c.JSON(http.StatusOK, migrationsSuccessResponse{results, appliedMigrations})
}

// SetupRouter setups router
func SetupRouter(config *config.Config, newCoordinator func(ctx context.Context, config *config.Config) coordinator.Coordinator) *gin.Engine {
	r := gin.New()
	r.HandleMethodNotAllowed = true
	r.Use(recovery(), requestIDHandler(), requestLoggerHandler())

	v1 := r.Group("/v1")

	v1.GET("/config", makeHandler(config, newCoordinator, configHandler))

	v1.GET("/tenants", makeHandler(config, newCoordinator, tenantsGetHandler))
	v1.POST("/tenants", makeHandler(config, newCoordinator, tenantsPostHandler))

	v1.GET("/migrations/source", makeHandler(config, newCoordinator, migrationsSourceHandler))
	v1.GET("/migrations/applied", makeHandler(config, newCoordinator, migrationsAppliedHandler))
	v1.POST("/migrations", makeHandler(config, newCoordinator, migrationsPostHandler))

	return r
}
