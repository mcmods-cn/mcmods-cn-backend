package httpapi

import (
	"context"
	"testing"
	"time"

	natsserver "github.com/nats-io/nats-server/v2/server"

	"mcmods-cn-backend/internal/config"
	"mcmods-cn-backend/internal/queue"
)

func TestBUG046RealtimeBroadcastDeliversOncePerInstance(t *testing.T) {
	options := &natsserver.Options{Host: "127.0.0.1", Port: -1, NoLog: true, NoSigs: true}
	broker, err := natsserver.NewServer(options)
	if err != nil {
		t.Fatal(err)
	}
	go broker.Start()
	if !broker.ReadyForConnections(5 * time.Second) {
		broker.Shutdown()
		t.Fatal("test NATS server did not become ready")
	}
	defer broker.Shutdown()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	queueConfig := config.NATSConfig{
		Enabled: true, URL: broker.ClientURL(), SubjectPrefix: "bug046", Realtime: true,
	}
	firstQueue := queue.New(ctx, queueConfig)
	defer firstQueue.Close()
	secondQueue := queue.New(ctx, queueConfig)
	defer secondQueue.Close()
	if !firstQueue.Status().Connected || !secondQueue.Status().Connected {
		t.Fatalf("test NATS clients are not connected: first=%#v second=%#v", firstQueue.Status(), secondQueue.Status())
	}
	first := &Server{queue: firstQueue, realtime: newRealtimeHub()}
	second := &Server{queue: secondQueue, realtime: newRealtimeHub()}
	first.subscribeRealtimeBroadcast()
	second.subscribeRealtimeBroadcast()
	firstEvents, unsubscribeFirst := first.realtime.subscribe(42)
	defer unsubscribeFirst()
	secondEvents, unsubscribeSecond := second.realtime.subscribe(42)
	defer unsubscribeSecond()

	first.publishRealtimeUser(42, "message.created", map[string]string{"messageId": "m00000042"})
	firstEvent := receiveBUG046RealtimeEvent(t, firstEvents, "origin instance")
	secondEvent := receiveBUG046RealtimeEvent(t, secondEvents, "peer instance")
	if firstEvent.ID == "" || firstEvent.ID != secondEvent.ID || firstEvent.Type != "message.created" || secondEvent.Type != firstEvent.Type || firstEvent.Origin == "" || firstEvent.Origin != secondEvent.Origin {
		t.Fatalf("broadcast identity diverged: first=%#v second=%#v", firstEvent, secondEvent)
	}
	assertNoBUG046RealtimeEvent(t, firstEvents, "origin instance received its NATS echo")
	assertNoBUG046RealtimeEvent(t, secondEvents, "peer instance received a duplicate")

	second.publishRealtimeUser(42, "message.updated", map[string]string{"messageId": "m00000042"})
	firstReturnEvent := receiveBUG046RealtimeEvent(t, firstEvents, "first instance receiving peer event")
	secondOriginEvent := receiveBUG046RealtimeEvent(t, secondEvents, "second origin instance")
	if firstReturnEvent.ID == "" || firstReturnEvent.ID != secondOriginEvent.ID || firstReturnEvent.Type != "message.updated" || secondOriginEvent.Type != firstReturnEvent.Type || firstReturnEvent.Origin == "" || firstReturnEvent.Origin != secondOriginEvent.Origin {
		t.Fatalf("reverse broadcast identity diverged: first=%#v second=%#v", firstReturnEvent, secondOriginEvent)
	}
	if firstReturnEvent.Origin == firstEvent.Origin {
		t.Fatalf("two server instances unexpectedly share an origin: first=%q second=%q", firstEvent.Origin, firstReturnEvent.Origin)
	}
	assertNoBUG046RealtimeEvent(t, firstEvents, "first peer instance received a duplicate")
	assertNoBUG046RealtimeEvent(t, secondEvents, "second origin instance received its NATS echo")

	legacyEvent := realtimeEvent{ID: "legacy-background-event", Type: "notification.created", Data: map[string]string{"notificationId": "n00000042"}}
	if err := firstQueue.PublishBroadcast(context.Background(), "user.42", legacyEvent); err != nil {
		t.Fatalf("publish origin-less compatibility event: %v", err)
	}
	firstLegacyEvent := receiveBUG046RealtimeEvent(t, firstEvents, "first instance receiving origin-less event")
	secondLegacyEvent := receiveBUG046RealtimeEvent(t, secondEvents, "second instance receiving origin-less event")
	if firstLegacyEvent.ID != legacyEvent.ID || secondLegacyEvent.ID != legacyEvent.ID || firstLegacyEvent.Type != legacyEvent.Type || secondLegacyEvent.Type != legacyEvent.Type || firstLegacyEvent.Origin != "" || secondLegacyEvent.Origin != "" {
		t.Fatalf("origin-less compatibility broadcast diverged: first=%#v second=%#v", firstLegacyEvent, secondLegacyEvent)
	}
	assertNoBUG046RealtimeEvent(t, firstEvents, "first instance received a duplicate origin-less event")
	assertNoBUG046RealtimeEvent(t, secondEvents, "second instance received a duplicate origin-less event")
}

func receiveBUG046RealtimeEvent(t *testing.T, events <-chan realtimeEvent, location string) realtimeEvent {
	t.Helper()
	select {
	case event := <-events:
		return event
	case <-time.After(2 * time.Second):
		t.Fatalf("%s did not receive the realtime event", location)
		return realtimeEvent{}
	}
}

func assertNoBUG046RealtimeEvent(t *testing.T, events <-chan realtimeEvent, message string) {
	t.Helper()
	select {
	case event := <-events:
		t.Fatalf("%s: %#v", message, event)
	case <-time.After(150 * time.Millisecond):
	}
}
