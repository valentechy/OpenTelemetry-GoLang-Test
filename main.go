package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("Error: %v", err)
	}
}

func run() (err error) {
	// Manejando la señal CTRL+C
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Configuración OpenTelemetry
	otelShutdown, err := setupOTelSDK(context.Background())
	if err != nil {
		log.Fatalf("Fallo al configurar OpenTelemetry SDK: %v", err)
	}

	defer func() {
		err = errors.Join(err, otelShutdown(context.Background()))
	}()

	// Raiz
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "Hola, Mundo!")
	})

	srv := &http.Server{
		Addr:         ":8080",
		BaseContext:  func(listener net.Listener) context.Context { return ctx },
		ReadTimeout:  time.Second,
		WriteTimeout: 10 * time.Second,
		Handler:      newHTTPHandler(),
	}

	srvErr := make(chan error, 1)
	go func() {
		srvErr <- srv.ListenAndServe()
	}()

	// Esperando para interrupciones
	select {
	case err = <-srvErr:
		// Error inicializando el servidor
		return
	case <-ctx.Done():
		// Esperando a CTRL+C
		stop()
	}

	err = srv.Shutdown(context.Background())
	return
}

func newHTTPHandler() http.Handler {
	mux := http.NewServeMux()

	// handleFunc es sustituto de HandleFunc
	handleFunc := func(pattern string, f func(http.ResponseWriter, *http.Request)) {
		// Configurando http.route para la instrumentacion de HTTP
		handler := otelhttp.WithRouteTag(pattern, http.HandlerFunc(f))
		mux.Handle(pattern, handler)
	}

	// Registrando rutas
	handleFunc("/", hello)
	handleFunc("/dice", dice)

	// Agregar instrumentación a todo el servidor
	handler := otelhttp.NewHandler(mux, "/")
	return handler
}

const name = "go.opentelemetry.io/otel/example/dice"

var (
	tracer  = otel.Tracer(name)
	meter   = otel.Meter(name)
	logger  = otelslog.NewLogger(name)
	rollCnt metric.Int64Counter
)

func init() {
	var err error
	rollCnt, err = meter.Int64Counter("dice.rolls",
		metric.WithDescription("The total number of dice rolls"),
		metric.WithUnit("{roll}"))

	if err != nil {
		panic(err)
	}
}

func hello(w http.ResponseWriter, r *http.Request) {
	log.Println("Hola, Mundo!")
}

func dice(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "roll")
	defer span.End()

	roll := 1 + rand.Intn(6)

	var msg string
	if player := r.PathValue("player"); player != "" {
		msg = fmt.Sprintf("%s is rolling the dice", player)
	} else {
		msg = "Anonymous player is rolling the dice"
	}
	logger.InfoContext(ctx, msg, "result", roll)

	rollValueAttr := attribute.Int("roll.value", roll)
	span.SetAttributes(rollValueAttr)
	rollCnt.Add(ctx, 1, metric.WithAttributes(rollValueAttr))

	resp := strconv.Itoa(roll) + "\n"
	if _, err := io.WriteString(w, resp); err != nil {
		log.Printf("Escritura fallida: %v\n", err)
	}
}
