# Temporal worker lab

One Kubernetes Deployment runs Go workers that poll the same Temporal task queue, `baseline-workers`. The HPA manages the replica count between 3 and 6.

## Deploy on Colima

Run these commands from the repository root. The worker uses the existing local Temporal server on port `7233`. Register the Temporal namespace once if it does not already exist.

```bash
temporal operator namespace create --address 127.0.0.1:7233 --namespace temporal-worker-demo --retention 24h
docker --context colima build -t temporal-worker-lab:0.2.0 .
kubectl --context colima apply --server-side -f k8s/worker.yaml -f k8s/hpa.yaml
kubectl --context colima -n temporal-worker-demo rollout status deployment/temporal-worker
```

## Run a workflow

The `WorkerIdentity` workflow returns the name of the pod that ran its activity.

```bash
temporal workflow execute --address 127.0.0.1:7233 --namespace temporal-worker-demo \
  --task-queue baseline-workers --type WorkerIdentity
```

To generate two minutes of CPU load, start `LongRunningWorkflow`. It runs three parallel activities and returns the names of the worker pods that processed them.

```bash
temporal workflow start --address 127.0.0.1:7233 --namespace temporal-worker-demo \
  --task-queue baseline-workers --type LongRunningWorkflow --input 120
kubectl --context colima -n temporal-worker-demo top pods
```

## Watch HPA scale the workers

The HPA targets 60% of each pod's `25m` CPU request, or `15m` average CPU per pod. Start four workflows to queue 12 activities, then watch the replica count.

```bash
for i in 1 2 3 4; do
  temporal workflow start --address 127.0.0.1:7233 --namespace temporal-worker-demo \
    --task-queue baseline-workers --type LongRunningWorkflow --input 60
done
kubectl --context colima -n temporal-worker-demo get hpa temporal-worker --watch
```

## Inspect VPA recommendations

With the VPA recommender installed, apply the VPA in `Off` mode. Compare its `Target` with the requests in `k8s/worker.yaml`. HPA continues using the configured CPU request to calculate utilization.

```bash
kubectl --context colima apply --server-side -f k8s/vpa.yaml
kubectl --context colima -n temporal-worker-demo describe vpa temporal-worker
```
