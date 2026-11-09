package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
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

// LongRunningWorkflow gives three worker pods CPU work for the requested duration.
func LongRunningWorkflow(ctx workflow.Context, seconds int) ([]string, error) {
	if seconds < 1 || seconds > 600 {
		return nil, fmt.Errorf("duration must be between 1 and 600 seconds")
	}
	duration := time.Duration(seconds) * time.Second
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: duration + 30*time.Second,
		HeartbeatTimeout:    20 * time.Second,
		RetryPolicy:         &temporal.RetryPolicy{MaximumAttempts: 1},
	})
	futures := make([]workflow.Future, 3)
	for i := range futures {
		futures[i] = workflow.ExecuteActivity(ctx, burnCPU, seconds)
	}
	pods := make([]string, len(futures))
	for i, future := range futures {
		if err := future.Get(ctx, &pods[i]); err != nil {
			return nil, err
		}
	}
	return pods, nil
}

func burnCPU(ctx context.Context, seconds int) (string, error) {
	hostname, err := os.Hostname()
	if err != nil {
		return "", err
	}
	started := time.Now()
	deadline := started.Add(time.Duration(seconds) * time.Second)
	nextHeartbeat := started
	sum := sha256.Sum256([]byte(hostname))
	for now := started; now.Before(deadline); now = time.Now() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !now.Before(nextHeartbeat) {
			activity.RecordHeartbeat(ctx, hostname)
			nextHeartbeat = now.Add(5 * time.Second)
		}
		for range 1024 {
			sum = sha256.Sum256(sum[:])
		}
	}
	return hostname, nil
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
	w.RegisterWorkflow(LongRunningWorkflow)
	w.RegisterActivity(burnCPU)
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
