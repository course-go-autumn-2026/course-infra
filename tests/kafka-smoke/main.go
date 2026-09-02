// Command kafka-smoke verifies host-side Kafka publish and consume behavior.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
)

func main() {
	endpoint := flag.String("endpoint", "localhost:24092", "Kafka bootstrap endpoint")
	topic := flag.String("topic", "trip.events.v1", "topic to test")
	message := flag.String("message", "tripgo-stage9", "message payload")
	groupFlag := flag.String("group", "", "consumer group (default: unique)")
	expectedKey := flag.String("expected-key", "", "require this record key (default: producer group when publishing; any key otherwise)")
	publish := flag.Bool("publish", true, "publish the message before consuming")
	timeout := flag.Duration("timeout", 30*time.Second, "operation timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	group := *groupFlag
	if group == "" {
		group = fmt.Sprintf("tripgo-smoke-%d", time.Now().UnixNano())
	}
	client, err := kgo.NewClient(
		kgo.SeedBrokers(*endpoint),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(*topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		fatal(err)
	}
	defer client.Close()
	if err := client.Ping(ctx); err != nil {
		fatal(fmt.Errorf("ping Kafka: %w", err))
	}
	if *publish {
		result := client.ProduceSync(ctx, &kgo.Record{Topic: *topic, Key: []byte(group), Value: []byte(*message)})
		if *expectedKey == "" {
			*expectedKey = group
		}
		if err := result.FirstErr(); err != nil {
			fatal(fmt.Errorf("publish: %w", err))
		}
	}
	for ctx.Err() == nil {
		fetches := client.PollFetches(ctx)
		if errs := fetches.Errors(); len(errs) != 0 {
			fatal(fmt.Errorf("consume: %v", errs))
		}
		found := false
		fetches.EachRecord(func(record *kgo.Record) {
			if recordMatches(record, *message, *expectedKey) {
				found = true
			}
		})
		if found {
			fmt.Printf("observed %q through %s (publish=%t)\n", *message, *endpoint, *publish)
			return
		}
	}
	fatal(fmt.Errorf("consume matching record: %w", ctx.Err()))
}

func recordMatches(record *kgo.Record, message, expectedKey string) bool {
	return string(record.Value) == message && (expectedKey == "" || string(record.Key) == expectedKey)
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
