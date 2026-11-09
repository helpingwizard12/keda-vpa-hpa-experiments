package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

// WorkerIdentity returns the name of the pod that executes the activity.
func WorkerIdentity(ctx workflow.Context) (string, error) {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 10 * time.Second,
	})
	var hostname string
	err := workflow.ExecuteActivity(ctx, reportIdentity).Get(ctx, &hostname)
	return hostname, err
}

func reportIdentity() (string, error) {
	return os.Hostname()
}

func run() error {
	address := os.Getenv("TEMPORAL_ADDRESS")
	namespace := os.Getenv("TEMPORAL_NAMESPACE")
	taskQueue := os.Getenv("TEMPORAL_TASK_QUEUE")
	if address == "" || namespace == "" || taskQueue == "" {
		return fmt.Errorf("TEMPORAL_ADDRESS, TEMPORAL_NAMESPACE, and TEMPORAL_TASK_QUEUE are required")
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c, err := client.DialContext(ctx, client.Options{HostPort: address, Namespace: namespace})
	if err != nil {
		return fmt.Errorf("connect to Temporal: %w", err)
	}
	defer c.Close()

	w := worker.New(c, taskQueue, worker.Options{
		MaxConcurrentActivityExecutionSize:     1,
		MaxConcurrentWorkflowTaskExecutionSize: 2,
		WorkerStopTimeout:                      10 * time.Second,
	})
	w.RegisterWorkflow(WorkerIdentity)
	w.RegisterActivity(reportIdentity)
	if err := w.Start(); err != nil {
		return fmt.Errorf("start worker: %w", err)
	}
	defer w.Stop()
	log.Printf("Polling %s in Temporal namespace %s", taskQueue, namespace)

	ready := http.NewServeMux()
	ready.HandleFunc("GET /ready", func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
	})
	server := &http.Server{Addr: ":8080", Handler: ready, ReadHeaderTimeout: 2 * time.Second}
	serveErrors := make(chan error, 1)
	go func() { serveErrors <- server.ListenAndServe() }()
	select {
	case <-ctx.Done():
		return server.Close()
	case err := <-serveErrors:
		return err
	}
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
