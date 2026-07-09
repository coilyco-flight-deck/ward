package main

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

func TestInitErrorReportingOffByDefault(t *testing.T) {
	oldInit := sentryInit
	t.Cleanup(func() {
		sentryInit = oldInit
		errorReportingActive = false
	})
	t.Setenv(envSentryDSN, "")
	called := false
	sentryInit = func(sentry.ClientOptions) error {
		called = true
		return nil
	}
	if initErrorReporting() {
		t.Fatal("initErrorReporting() = true with empty DSN")
	}
	if called {
		t.Fatal("sentry init ran with no DSN set")
	}
}

func TestReportRecoveredPanicRedactsAndRepanics(t *testing.T) {
	transport := &captureTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.com/1",
		Transport: transport,
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return scrubEvent(event)
		},
		Release: "ward@" + Version,
	})
	if err != nil {
		t.Fatalf("new sentry client: %v", err)
	}
	hub := sentry.NewHub(client, sentry.NewScope())

	secret := "ghp_0123456789abcdefghijklmnopqrstuvwxyz"
	wantErr := errors.New("ward panic with " + secret)

	func() {
		defer func() {
			if r := recover(); r != nil {
				if got, ok := r.(error); !ok || got.Error() != wantErr.Error() {
					t.Fatalf("repanic = %v; want %v", r, wantErr)
				}
			} else {
				t.Fatal("panic was swallowed")
			}
		}()
		defer func() {
			if r := recover(); r != nil {
				reportRecoveredPanic(hub, r)
				panic(r)
			}
		}()
		panic(wantErr)
	}()

	ev := transport.lastEvent()
	if ev == nil {
		t.Fatal("missing captured panic event")
	}
	if got := ev.Exception; len(got) == 0 {
		t.Fatal("missing exception payload in captured event")
	} else {
		if got[0].Value == "" || got[0].Value == wantErr.Error() {
			t.Fatalf("panic value was not scrubbed: %#v", got[0].Value)
		}
		if got[0].Value == "" || got[0].Value == secret {
			t.Fatalf("secret survived capture: %#v", got[0].Value)
		}
		if want := redactionPlaceholder; got[0].Value != "" && !strings.Contains(got[0].Value, want) {
			t.Fatalf("scrubbed exception missing %q: %#v", want, got[0].Value)
		}
	}
	if got := ev.Release; got != "ward@"+Version {
		t.Fatalf("release tag = %q; want %q", got, "ward@"+Version)
	}
}

type captureTransport struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (t *captureTransport) Configure(sentry.ClientOptions) {}

func (t *captureTransport) SendEvent(event *sentry.Event) {
	t.mu.Lock()
	defer t.mu.Unlock()
	clone := *event
	t.events = append(t.events, &clone)
}

func (t *captureTransport) Flush(time.Duration) bool { return true }

func (t *captureTransport) FlushWithContext(context.Context) bool { return true }

func (t *captureTransport) Close() {}

func (t *captureTransport) lastEvent() *sentry.Event {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.events) == 0 {
		return nil
	}
	return t.events[len(t.events)-1]
}
