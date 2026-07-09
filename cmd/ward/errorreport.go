package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/getsentry/sentry-go"
)

// ward-side crash reporting is optional and off by default. The operator layer
// resolves WARD_SENTRY_DSN from /sentry-dsn/ward and exports it when desired.
const envSentryDSN = "WARD_SENTRY_DSN"

// Optional deployment tag for the GlitchTip/Sentry project.
const envSentryEnvironment = "WARD_SENTRY_ENVIRONMENT"

const sentryFlushTimeout = 2 * time.Second

var sentryInit = sentry.Init

var errorReportingActive bool

func initErrorReporting() bool {
	dsn := strings.TrimSpace(os.Getenv(envSentryDSN))
	if dsn == "" {
		errorReportingActive = false
		return false
	}
	if err := sentryInit(sentry.ClientOptions{
		Dsn:         dsn,
		Release:     "ward@" + Version,
		Environment: strings.TrimSpace(os.Getenv(envSentryEnvironment)),
		BeforeSend: func(event *sentry.Event, _ *sentry.EventHint) *sentry.Event {
			return scrubEvent(event)
		},
	}); err != nil {
		fmt.Fprintf(os.Stderr, "ward: crash reporting disabled (%v)\n", err)
		errorReportingActive = false
		return false
	}
	errorReportingActive = true
	configureCrashReportingScope(os.Args)
	return true
}

func configureCrashReportingScope(argv []string) {
	if !errorReportingActive {
		return
	}
	sentry.CurrentHub().ConfigureScope(func(scope *sentry.Scope) {
		scope.SetTag("ward.version", Version)
		scope.SetTag("ward.verb", invokedVerb(argv))
		scope.SetTag("goos", runtime.GOOS)
		scope.SetTag("goarch", runtime.GOARCH)
		if h, err := os.Hostname(); err == nil && h != "" {
			scope.SetTag("host", h)
		}
	})
}

func reportPanic() {
	r := recover()
	if r == nil {
		return
	}
	if errorReportingActive {
		reportRecoveredPanic(sentry.CurrentHub(), r)
	}
	panic(r)
}

func reportRecoveredPanic(hub *sentry.Hub, recovered any) {
	if hub == nil || recovered == nil {
		return
	}
	hub.Recover(recovered)
	hub.Flush(sentryFlushTimeout)
}

func scrubEvent(event *sentry.Event) *sentry.Event {
	if event == nil {
		return nil
	}
	event.Message = redactSecrets(event.Message)
	event.Dist = redactSecrets(event.Dist)
	event.Environment = redactSecrets(event.Environment)
	event.Logger = redactSecrets(event.Logger)
	event.Release = redactSecrets(event.Release)
	event.ServerName = redactSecrets(event.ServerName)
	event.Transaction = redactSecrets(event.Transaction)
	for i := range event.Fingerprint {
		event.Fingerprint[i] = redactSecrets(event.Fingerprint[i])
	}
	for i := range event.Tags {
		event.Tags[i] = redactSecrets(event.Tags[i])
	}
	for i := range event.Modules {
		event.Modules[i] = redactSecrets(event.Modules[i])
	}
	for i := range event.Exception {
		event.Exception[i].Type = redactSecrets(event.Exception[i].Type)
		event.Exception[i].Value = redactSecrets(event.Exception[i].Value)
		if mech := event.Exception[i].Mechanism; mech != nil {
			mech.Description = redactSecrets(mech.Description)
			mech.HelpLink = redactSecrets(mech.HelpLink)
			mech.Source = redactSecrets(mech.Source)
			mech.Data = scrubStringMapAny(mech.Data)
		}
	}
	for _, bc := range event.Breadcrumbs {
		if bc == nil {
			continue
		}
		bc.Message = redactSecrets(bc.Message)
		bc.Data = scrubStringMapAny(bc.Data)
	}
	if event.Request != nil {
		event.Request.URL = redactSecrets(event.Request.URL)
		event.Request.Method = redactSecrets(event.Request.Method)
		event.Request.Data = redactSecrets(event.Request.Data)
		event.Request.QueryString = redactSecrets(event.Request.QueryString)
		event.Request.Cookies = redactSecrets(event.Request.Cookies)
		event.Request.Headers = scrubStringMap(event.Request.Headers)
		event.Request.Env = scrubStringMap(event.Request.Env)
	}
	event.User.Email = redactSecrets(event.User.Email)
	event.User.ID = redactSecrets(event.User.ID)
	event.User.IPAddress = redactSecrets(event.User.IPAddress)
	event.User.Name = redactSecrets(event.User.Name)
	event.User.Username = redactSecrets(event.User.Username)
	event.User.Data = scrubStringMap(event.User.Data)
	event.Contexts = scrubContexts(event.Contexts)
	return event
}

func scrubContexts(contexts map[string]sentry.Context) map[string]sentry.Context {
	if len(contexts) == 0 {
		return contexts
	}
	for k, v := range contexts {
		contexts[k] = scrubStringMapAny(v)
	}
	return contexts
}

func scrubStringMap(m map[string]string) map[string]string {
	if len(m) == 0 {
		return m
	}
	for k, v := range m {
		m[k] = redactSecrets(v)
	}
	return m
}

func scrubStringMapAny(m map[string]any) map[string]any {
	if len(m) == 0 {
		return m
	}
	for k, v := range m {
		m[k] = scrubAny(v)
	}
	return m
}

func scrubAny(v any) any {
	switch x := v.(type) {
	case string:
		return redactSecrets(x)
	case map[string]string:
		return scrubStringMap(x)
	case map[string]any:
		return scrubStringMapAny(x)
	case []string:
		out := make([]string, len(x))
		for i, s := range x {
			out[i] = redactSecrets(s)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			out[i] = scrubAny(item)
		}
		return out
	default:
		return v
	}
}

func invokedVerb(args []string) string {
	for i := 1; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return "unknown"
		}
		if strings.HasPrefix(a, "-") {
			if rootValueFlags[a] && i+1 < len(args) {
				i++
			}
			continue
		}
		return a
	}
	return "unknown"
}
