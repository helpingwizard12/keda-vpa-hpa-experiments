# Temporal worker lab

The baseline Deployment runs Go workers on the `baseline-workers` Temporal task queue. Its CPU HPA scales it between 3 and 6 replicas. The KEDA experiment runs the same worker on a separate queue.

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

## Apply VPA recommendations

Install the VPA recommender, updater, and admission controller before enabling this policy. VPA adjusts the memory request up to `64Mi`. HPA uses the `25m` CPU request to calculate utilization. `InPlaceOrRecreate` tries to resize a running pod and can replace it if needed. The disruption budget allows at most one worker to become unavailable through voluntary evictions.

```bash
kubectl --context colima apply --server-side -f k8s/pdb.yaml
kubectl --context colima -n temporal-worker-demo wait --for=jsonpath='{.status.disruptionsAllowed}'=1 pdb/temporal-worker
kubectl --context colima apply --server-side -f k8s/vpa.yaml
kubectl --context colima -n temporal-worker-demo describe vpa temporal-worker
kubectl --context colima -n temporal-worker-demo get pods -l app.kubernetes.io/name=temporal-worker \
  -o 'custom-columns=NAME:.metadata.name,MEMORY:.status.containerStatuses[0].resources.requests.memory'
```

## Scale workers with KEDA

With KEDA 2.21 installed, apply `k8s/keda.yaml` after creating the lab namespace and building the worker image. This deploys a separate worker on `keda-workers`. KEDA wakes the first replica and creates an HPA that targets two queued Temporal tasks per worker, up to four replicas. The scaler counts queued workflow and activity tasks. Running workflows keep the scaler active so that it does not scale the deployment to zero; they do not increase the replica target. KEDA waits 45 seconds after it last saw activity before scaling to zero.

The HPA uses a 120-second scale-down stabilization window for this test. This gives the 30-second activities time to finish after workers take them off the queue. A shorter window of 45 seconds removed two busy workers during the first run, and two workflows timed out. A longer window helps this test, but it does not guarantee that arbitrary activities finish before a worker stops.

```bash
kubectl --context colima apply --server-side -f k8s/keda.yaml
for i in 1 2 3 4; do
  temporal workflow start --address 127.0.0.1:7233 --namespace temporal-worker-demo \
    --task-queue keda-workers --type LongRunningWorkflow --input 30
done
kubectl --context colima -n temporal-worker-demo get deployment temporal-worker-keda --watch
```

In the local Grafana dashboard, compare **KEDA · Temporal queue backlog** with **KEDA · Worker replicas (0–4)**. To repeat a cold-start test, wait until `kubectl --context colima -n temporal-worker-demo get pods -l app.kubernetes.io/name=temporal-worker-keda` shows no pods. The desired replica count reaches zero before the last pod exits.
