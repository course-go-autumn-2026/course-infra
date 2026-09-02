// Package redpanda defines the managed topic reconciliation contract.
package redpanda

// Topics is the exact desired topic-to-partition configuration mounted into the reconciler.
const Topics = `trip.events.v1=3
trip.commands.v1=3
trip.commands.v1.dlq=1
`

// ReconcileScript continuously creates missing topics, increases undersized
// topics, and refuses destructive partition reduction.
const ReconcileScript = `#!/bin/bash
set -euo pipefail
broker=redpanda:9092
reconcile() {
  rm -f /tmp/topics-ready
  while IFS== read -r topic desired; do
    [ -n "$topic" ] || continue
    if ! rpk topic list --brokers "$broker" | awk -v wanted="$topic" '$1 == wanted {found=1} END {exit !found}'; then
      rpk topic create "$topic" --partitions "$desired" --replicas 1 --brokers "$broker"
      current=$desired
    else
      rpk topic describe "$topic" -p --brokers "$broker" >/tmp/partitions
      # The partition table has one numeric data row per partition.
      current=$(awk '$1 ~ /^[0-9]+$/ {count++} END {print count+0}' /tmp/partitions)
    fi
    if [ "$current" -lt "$desired" ]; then
      rpk topic add-partitions "$topic" --num "$((desired-current))" --brokers "$broker"
    elif [ "$current" -gt "$desired" ]; then
      echo "topic $topic has $current partitions; refusing destructive reduction to $desired" >&2
      return 1
    fi
  done </config/topics
  touch /tmp/topics-ready
}
while true; do
  reconcile
  sleep 15
done
`

// ConfigMapData returns an independent exact data map for status validation.
func ConfigMapData() map[string]string {
	return map[string]string{"topics": Topics, "reconcile.sh": ReconcileScript}
}
