# Temporal worker lab

The baseline is one Kubernetes Deployment with 3 Go workers polling the same Temporal task queue, `baseline-workers`. The Deployment manifest sets the replica count.

## Deploy on Colima

Run these commands from the repository root. The worker uses the existing local Temporal server on port `7233`. Register the Temporal namespace once if it does not already exist.

```bash
temporal operator namespace create --address 127.0.0.1:7233 --namespace temporal-worker-demo --retention 24h
docker --context colima build -t temporal-worker-lab:0.1.0 .
kubectl --context colima apply -f k8s/worker.yaml
kubectl --context colima -n temporal-worker-demo rollout status deployment/temporal-worker
```

## Run a workflow

The `WorkerIdentity` workflow returns the name of the pod that ran its activity.

```bash
temporal workflow execute --address 127.0.0.1:7233 --namespace temporal-worker-demo \
  --task-queue baseline-workers --type WorkerIdentity
```
