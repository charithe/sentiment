package main

import (
	"context"
	"flag"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/charithe/sentiment"
	isatty "github.com/mattn/go-isatty"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

const httpTimeout = 10 * time.Second

var (
	cacheEntryTTL  = flag.Duration("cache_entry_ttl", 10*time.Minute, "TTL of cache entries")
	cacheMaxSizeMB = flag.Int("cache_max_size_mb", 64, "Maximum size of the cache")
	listenAddr     = flag.String("listen", ":8080", "Listen address")
	logLevel       = flag.String("log_level", "INFO", "Log level")
	requestTimeout = flag.Duration("timeout", 1*time.Second, "Timeout for requests")
)

func main() {
	flag.Parse()
	initLogging()

	sentimentSvc, err := sentiment.NewService(
		sentiment.WithCacheEntryTTL(*cacheEntryTTL),
		sentiment.WithCacheMaxSizeMB(*cacheMaxSizeMB),
		sentiment.WithRequestTimeout(*requestTimeout),
	)

	if err != nil {
		zap.S().Fatalw("Failed to initialize Sentiment service", "error", err)
	}

	defer sentimentSvc.Close()

	httpServer := startHTTPServer(sentimentSvc)

	shutdownChan := make(chan os.Signal, 1)
	signal.Notify(shutdownChan, os.Interrupt)
	<-shutdownChan

	zap.S().Info("Shutting down")
	ctx, cancelFunc := context.WithTimeout(context.Background(), 1*time.Minute)
	defer cancelFunc()
	httpServer.Shutdown(ctx)
}

func initLogging() {
	var logger *zap.Logger
	var err error
	errorPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl >= zapcore.ErrorLevel
	})

	minLogLevel := zapcore.InfoLevel
	switch strings.ToUpper(*logLevel) {
	case "DEBUG":
		minLogLevel = zapcore.DebugLevel
	case "INFO":
		minLogLevel = zapcore.InfoLevel
	case "WARN":
		minLogLevel = zapcore.WarnLevel
	case "ERROR":
		minLogLevel = zapcore.ErrorLevel
	}

	infoPriority := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl < zapcore.ErrorLevel && lvl >= minLogLevel
	})

	consoleErrors := zapcore.Lock(os.Stderr)
	consoleInfo := zapcore.Lock(os.Stdout)

	var consoleEncoder zapcore.Encoder
	if isatty.IsTerminal(os.Stdout.Fd()) {
		encoderConf := zap.NewDevelopmentEncoderConfig()
		encoderConf.EncodeLevel = zapcore.CapitalColorLevelEncoder
		consoleEncoder = zapcore.NewConsoleEncoder(encoderConf)
	} else {
		encoderConf := zap.NewProductionEncoderConfig()
		encoderConf.MessageKey = "message"
		encoderConf.EncodeTime = zapcore.TimeEncoder(zapcore.ISO8601TimeEncoder)
		consoleEncoder = zapcore.NewJSONEncoder(encoderConf)
	}

	core := zapcore.NewTee(
		zapcore.NewCore(consoleEncoder, consoleErrors, errorPriority),
		zapcore.NewCore(consoleEncoder, consoleInfo, infoPriority),
	)

	host, err := os.Hostname()
	if err != nil {
		host = "unknown"
	}

	stackTraceEnabler := zap.LevelEnablerFunc(func(lvl zapcore.Level) bool {
		return lvl > zapcore.ErrorLevel
	})
	logger = zap.New(core, zap.Fields(zap.String("host", host)), zap.AddStacktrace(stackTraceEnabler))

	if err != nil {
		zap.S().Fatalw("failed to create logger", "error", err)
	}

	zap.ReplaceGlobals(logger.Named("sentiment"))
	zap.RedirectStdLog(logger.Named("stdlog"))
}

func startHTTPServer(sentimentSvc *sentiment.Service) *http.Server {
	httpServer := &http.Server{
		Addr:              *listenAddr,
		Handler:           sentimentSvc.RESTHandler(),
		ErrorLog:          zap.NewStdLog(zap.L().Named("http")),
		ReadHeaderTimeout: httpTimeout,
		WriteTimeout:      httpTimeout,
		IdleTimeout:       httpTimeout,
	}

	go func() {
		zap.S().Infow("Starting HTTP server")
		if err := httpServer.ListenAndServe(); err != http.ErrServerClosed {
			zap.S().Fatalw("Failed to start HTTP server", "error", err)
		}
	}()

	return httpServer
}
