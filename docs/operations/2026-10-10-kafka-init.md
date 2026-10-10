# Kafka-init memory investigation (#320)

## Confirmed defect and change

`kafka-init` had a 64 MiB container limit without `KAFKA_HEAP_OPTS`.
[Kafka 3.9.0's CLI launcher](https://github.com/apache/kafka/blob/3.9.0/bin/kafka-run-class.sh#L257-L260)
defaults to `-Xmx256M`; heap size also excludes JVM native memory.

The init service now explicitly uses `-Xms32m -Xmx128m` and a 256 MiB
container limit. It exits after creating the five topics. Broker settings,
topic settings, retries and persistent volumes remain unchanged.

## Local verification, 2026-10-10

Docker Desktop on arm64, official `apache/kafka:3.9.0`, isolated compose
project `megagames-task320-check`. Only the production `kafka` and
`kafka-init` service definitions were used, with example topic variables,
no published ports and a fresh broker data directory in tmpfs. The broker
healthcheck interval was shortened to 5s for the experiment. Both
initializers ran the same production five-topic entrypoint against the
same ready broker. An additional trial disabled swap to show the memory
requirement independently of swap availability.

| Trial | Time | Exit | Memory peak | Swap peak |
|---|---:|---:|---:|---:|
| Old 64 MiB limit, default CLI heap | 6.81s | 0 | 64 MiB | 37 MiB |
| Old 64 MiB limit, swap disabled | 0.96s | 137, OOMKilled | Not sampled at failure | 0 |
| 256 MiB limit, heap 32–128 MiB | 5.60s | 0 | 87 MiB | 0 |
| Fresh broker + corrected initializer | 12.30s total | 0 | Not sampled | Not sampled |

A previous comparison measured 95 MiB peak with the corrected initializer
and no swap. The cold-start trial created all five topics and printed
`topics ready`. The repeated initialization used `--if-not-exists` and
also succeeded. All experiment containers, their anonymous volumes and
the separate test network were removed; no production volume was used.

## Limits of the conclusion

This confirms that 64 MiB requires swap or fails even with a ready
broker. It does not reproduce the two `createTopics` node-assignment
timeouts on the Raspberry Pi after its month-long downtime.

Read-only Pi logs show the broker entered RUNNING at 20:42:46 MSK, then
had controller-heartbeat RPC timeouts while deleting old metadata
snapshots. The initializer timed out at 20:45:12 and 20:47:45; after its
runtime memory limit was temporarily raised to 256 MiB it succeeded.
Memory pressure and broker recovery overlapped, so those observations
cannot establish memory as the sole cause of the timeouts.

The fix removes the demonstrated initializer memory pressure. Verify
`kafka-init` exit=0 and app health on the next approved Pi deployment;
record any further controller or initialization timeouts separately.
The local experiment used Apple arm64, not the Pi's Cortex-A72, and no
restart or diagnostic container was run on the live Pi for this task.
